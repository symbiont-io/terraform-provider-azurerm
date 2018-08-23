package azurerm

import (
	"context"
	"fmt"
	"log"
	"time"

	"regexp"

	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/helper/validation"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/tf"
)

func resourceArmStorageContainer() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmStorageContainerCreate,
		Read:   resourceArmStorageContainerRead,
		Exists: resourceArmStorageContainerExists,
		Delete: resourceArmStorageContainerDelete,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(time.Minute * 30),
			Delete: schema.DefaultTimeout(time.Minute * 30),
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validateArmStorageContainerName,
			},
			"resource_group_name": resourceGroupNameSchema(),
			"storage_account_name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"container_access_type": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Default:  "private",
				ValidateFunc: validation.StringInSlice(
					[]string{
						"blob",
						"container",
						"private",
					}, true),
			},
			"properties": {
				Type:     schema.TypeMap,
				Computed: true,
			},
		},
	}
}

func resourceArmStorageContainerCreate(d *schema.ResourceData, meta interface{}) error {
	armClient := meta.(*ArmClient)
	ctx := armClient.StopContext

	resourceGroupName := d.Get("resource_group_name").(string)
	storageAccountName := d.Get("storage_account_name").(string)

	waitCtx, cancel := context.WithTimeout(ctx, d.Timeout(schema.TimeoutCreate))
	defer cancel()
	blobClient, accountExists, err := armClient.getBlobStorageClientForStorageAccount(waitCtx, resourceGroupName, storageAccountName)
	if err != nil {
		return err
	}
	if !accountExists {
		return fmt.Errorf("Storage Account %q Not Found", storageAccountName)
	}

	name := d.Get("name").(string)

	var accessType storage.ContainerAccessType
	if d.Get("container_access_type").(string) == "private" {
		accessType = storage.ContainerAccessType("")
	} else {
		accessType = storage.ContainerAccessType(d.Get("container_access_type").(string))
	}

	reference := blobClient.GetContainerReference(name)
	exists, err := reference.Exists()
	if err != nil {
		return fmt.Errorf("Error checking if container %q exists in storage account %q: %+v", name, storageAccountName, err)
	}

	if exists {
		return tf.ImportAsExistsError("azurerm_storage_container", name)
	}

	log.Printf("[INFO] Creating container %q in storage account %q.", name, storageAccountName)
	err = resource.Retry(120*time.Second, checkContainerIsCreated(reference))
	if err != nil {
		return fmt.Errorf("Error creating container %q in storage account %q: %s", name, storageAccountName, err)
	}

	permissions := storage.ContainerPermissions{
		AccessType: accessType,
	}
	permissionOptions := &storage.SetContainerPermissionOptions{}
	err = reference.SetPermissions(permissions, permissionOptions)
	if err != nil {
		return fmt.Errorf("Error setting permissions for container %s in storage account %s: %+v", name, storageAccountName, err)
	}

	// TODO: fix the ID to be https://storageaccount.blob.core..../name and parse it
	d.SetId(name)
	return resourceArmStorageContainerRead(d, meta)
}

func resourceArmStorageContainerRead(d *schema.ResourceData, meta interface{}) error {
	armClient := meta.(*ArmClient)
	ctx := armClient.StopContext

	resourceGroupName := d.Get("resource_group_name").(string)
	storageAccountName := d.Get("storage_account_name").(string)

	blobClient, accountExists, err := armClient.getBlobStorageClientForStorageAccount(ctx, resourceGroupName, storageAccountName)
	if err != nil {
		return err
	}
	if !accountExists {
		log.Printf("[DEBUG] Storage account %q not found, removing container %q from state", storageAccountName, d.Id())
		d.SetId("")
		return nil
	}

	name := d.Get("name").(string)
	containers, err := blobClient.ListContainers(storage.ListContainersParameters{
		Prefix:  name,
		Timeout: 90,
	})
	if err != nil {
		return fmt.Errorf("Failed to retrieve storage containers in account %q: %s", name, err)
	}

	var found bool
	for _, cont := range containers.Containers {
		if cont.Name == name {
			found = true

			props := make(map[string]interface{})
			props["last_modified"] = cont.Properties.LastModified
			props["lease_status"] = cont.Properties.LeaseStatus
			props["lease_state"] = cont.Properties.LeaseState
			props["lease_duration"] = cont.Properties.LeaseDuration

			d.Set("properties", props)
		}
	}

	if !found {
		log.Printf("[INFO] Storage container %q does not exist in account %q, removing from state...", name, storageAccountName)
		d.SetId("")
	}

	return nil
}

func resourceArmStorageContainerExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	armClient := meta.(*ArmClient)
	ctx := armClient.StopContext

	resourceGroupName := d.Get("resource_group_name").(string)
	storageAccountName := d.Get("storage_account_name").(string)

	blobClient, accountExists, err := armClient.getBlobStorageClientForStorageAccount(ctx, resourceGroupName, storageAccountName)
	if err != nil {
		return false, err
	}
	if !accountExists {
		log.Printf("[DEBUG] Storage account %q not found, removing container %q from state", storageAccountName, d.Id())
		d.SetId("")
		return false, nil
	}

	name := d.Get("name").(string)

	log.Printf("[INFO] Checking existence of storage container %q in storage account %q", name, storageAccountName)
	reference := blobClient.GetContainerReference(name)
	exists, err := reference.Exists()
	if err != nil {
		return false, fmt.Errorf("Error querying existence of storage container %q in storage account %q: %s", name, storageAccountName, err)
	}

	if !exists {
		log.Printf("[INFO] Storage container %q does not exist in account %q, removing from state...", name, storageAccountName)
		d.SetId("")
	}

	return exists, nil
}

func resourceArmStorageContainerDelete(d *schema.ResourceData, meta interface{}) error {
	armClient := meta.(*ArmClient)
	ctx := armClient.StopContext

	resourceGroupName := d.Get("resource_group_name").(string)
	storageAccountName := d.Get("storage_account_name").(string)

	waitCtx, cancel := context.WithTimeout(ctx, d.Timeout(schema.TimeoutDelete))
	defer cancel()
	blobClient, accountExists, err := armClient.getBlobStorageClientForStorageAccount(waitCtx, resourceGroupName, storageAccountName)
	if err != nil {
		return err
	}
	if !accountExists {
		log.Printf("[INFO] Storage Account %q doesn't exist so the container won't exist", storageAccountName)
		return nil
	}

	name := d.Get("name").(string)

	log.Printf("[INFO] Deleting storage container %q in account %q", name, storageAccountName)
	reference := blobClient.GetContainerReference(name)
	deleteOptions := &storage.DeleteContainerOptions{}
	if _, err := reference.DeleteIfExists(deleteOptions); err != nil {
		return fmt.Errorf("Error deleting storage container %q from storage account %q: %s", name, storageAccountName, err)
	}

	return nil
}

func checkContainerIsCreated(reference *storage.Container) func() *resource.RetryError {
	return func() *resource.RetryError {
		createOptions := &storage.CreateContainerOptions{}
		_, err := reference.CreateIfNotExists(createOptions)
		if err != nil {
			return resource.RetryableError(err)
		}

		return nil
	}
}

//Following the naming convention as laid out in the docs
func validateArmStorageContainerName(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	if !regexp.MustCompile(`^\$root$|^[0-9a-z-]+$`).MatchString(value) {
		errors = append(errors, fmt.Errorf(
			"only lowercase alphanumeric characters and hyphens allowed in %q: %q",
			k, value))
	}
	if len(value) < 3 || len(value) > 63 {
		errors = append(errors, fmt.Errorf(
			"%q must be between 3 and 63 characters: %q", k, value))
	}
	if regexp.MustCompile(`^-`).MatchString(value) {
		errors = append(errors, fmt.Errorf(
			"%q cannot begin with a hyphen: %q", k, value))
	}
	return
}
