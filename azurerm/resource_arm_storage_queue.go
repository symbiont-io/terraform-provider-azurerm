package azurerm

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/tf"
)

func resourceArmStorageQueue() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmStorageQueueCreate,
		Read:   resourceArmStorageQueueRead,
		Exists: resourceArmStorageQueueExists,
		Delete: resourceArmStorageQueueDelete,
		// TODO: support import
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(time.Minute * 30),
			Delete: schema.DefaultTimeout(time.Minute * 30),
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validateArmStorageQueueName,
			},
			"resource_group_name": resourceGroupNameSchema(),
			"storage_account_name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
		},
	}
}

func resourceArmStorageQueueCreate(d *schema.ResourceData, meta interface{}) error {
	armClient := meta.(*ArmClient)
	ctx := armClient.StopContext

	resourceGroupName := d.Get("resource_group_name").(string)
	storageAccountName := d.Get("storage_account_name").(string)

	waitCtx, cancel := context.WithTimeout(ctx, d.Timeout(schema.TimeoutCreate))
	defer cancel()
	queueClient, accountExists, err := armClient.getQueueServiceClientForStorageAccount(waitCtx, resourceGroupName, storageAccountName)
	if err != nil {
		return err
	}
	if !accountExists {
		return fmt.Errorf("Storage Account %q Not Found", storageAccountName)
	}

	name := d.Get("name").(string)
	queueReference := queueClient.GetQueueReference(name)
	exists, err := queueReference.Exists()
	if err != nil {
		return fmt.Errorf("Error checking for the existence of queue %q in storage account %q: %+v", name, storageAccountName, err)
	}

	if exists {
		return tf.ImportAsExistsError("azurerm_storage_queue", name)
	}

	log.Printf("[INFO] Creating queue %q in storage account %q", name, storageAccountName)
	options := &storage.QueueServiceOptions{}
	err = queueReference.Create(options)
	if err != nil {
		return fmt.Errorf("Error creating storage queue on Azure: %s", err)
	}

	// TODO: fix the ID
	d.SetId(name)
	return resourceArmStorageQueueRead(d, meta)
}

func resourceArmStorageQueueRead(d *schema.ResourceData, meta interface{}) error {
	exists, err := resourceArmStorageQueueExists(d, meta)
	if err != nil {
		return err
	}

	if !exists {
		// Exists already removed this from state
		return nil
	}

	return nil
}

func resourceArmStorageQueueExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	armClient := meta.(*ArmClient)
	ctx := armClient.StopContext

	resourceGroupName := d.Get("resource_group_name").(string)
	storageAccountName := d.Get("storage_account_name").(string)

	queueClient, accountExists, err := armClient.getQueueServiceClientForStorageAccount(ctx, resourceGroupName, storageAccountName)
	if err != nil {
		return false, err
	}
	if !accountExists {
		log.Printf("[DEBUG] Storage account %q not found, removing queue %q from state", storageAccountName, d.Id())
		d.SetId("")
		return false, nil
	}

	name := d.Get("name").(string)

	log.Printf("[INFO] Checking for existence of storage queue %q.", name)
	queueReference := queueClient.GetQueueReference(name)
	exists, err := queueReference.Exists()
	if err != nil {
		return false, fmt.Errorf("error testing existence of storage queue %q: %s", name, err)
	}

	if !exists {
		log.Printf("[INFO] Storage queue %q no longer exists, removing from state...", name)
		d.SetId("")
	}

	return exists, nil
}

func resourceArmStorageQueueDelete(d *schema.ResourceData, meta interface{}) error {
	armClient := meta.(*ArmClient)
	ctx := armClient.StopContext

	resourceGroupName := d.Get("resource_group_name").(string)
	storageAccountName := d.Get("storage_account_name").(string)

	waitCtx, cancel := context.WithTimeout(ctx, d.Timeout(schema.TimeoutDelete))
	defer cancel()
	queueClient, accountExists, err := armClient.getQueueServiceClientForStorageAccount(waitCtx, resourceGroupName, storageAccountName)
	if err != nil {
		return err
	}
	if !accountExists {
		log.Printf("[INFO]Storage Account %q doesn't exist so the blob won't exist", storageAccountName)
		return nil
	}

	name := d.Get("name").(string)

	log.Printf("[INFO] Deleting storage queue %q", name)
	queueReference := queueClient.GetQueueReference(name)
	options := &storage.QueueServiceOptions{}
	if err = queueReference.Delete(options); err != nil {
		return fmt.Errorf("Error deleting storage queue %q: %s", name, err)
	}

	return nil
}

func validateArmStorageQueueName(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)

	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(value) {
		errors = append(errors, fmt.Errorf(
			"only lowercase alphanumeric characters and hyphens allowed in %q", k))
	}

	if regexp.MustCompile(`^-`).MatchString(value) {
		errors = append(errors, fmt.Errorf("%q cannot start with a hyphen", k))
	}

	if regexp.MustCompile(`-$`).MatchString(value) {
		errors = append(errors, fmt.Errorf("%q cannot end with a hyphen", k))
	}

	if len(value) > 63 {
		errors = append(errors, fmt.Errorf(
			"%q cannot be longer than 63 characters", k))
	}

	if len(value) < 3 {
		errors = append(errors, fmt.Errorf(
			"%q must be at least 3 characters", k))
	}

	return
}
