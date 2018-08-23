package azurerm

import (
	"fmt"
	"log"
	"regexp"

	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/tf"
)

func resourceArmStorageTable() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmStorageTableCreate,
		Read:   resourceArmStorageTableRead,
		Delete: resourceArmStorageTableDelete,
		// TODO: import support

		Schema: map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validateArmStorageTableName,
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

func resourceArmStorageTableCreate(d *schema.ResourceData, meta interface{}) error {
	armClient := meta.(*ArmClient)
	ctx := armClient.StopContext

	name := d.Get("name").(string)
	resourceGroupName := d.Get("resource_group_name").(string)
	storageAccountName := d.Get("storage_account_name").(string)

	tableClient, accountExists, err := armClient.getTableServiceClientForStorageAccount(ctx, resourceGroupName, storageAccountName)
	if err != nil {
		return err
	}
	if !accountExists {
		return fmt.Errorf("Storage Account %q Not Found", storageAccountName)
	}

	// firstly check if the table already exists and needs importing
	tables, err := tableClient.QueryTables(storage.MinimalMetadata, &storage.QueryTablesOptions{})
	if err != nil {
		return fmt.Errorf("Failed to retrieve storage tables in account %q: %s", name, err)
	}

	for _, t := range tables.Tables {
		if t.Name == name {
			return tf.ImportAsExistsError("azurerm_storage_table", t.Name)
		}
	}

	log.Printf("[INFO] Creating table %q in storage account %q.", name, storageAccountName)
	table := tableClient.GetTableReference(name)
	timeout := uint(60)
	options := &storage.TableOptions{}
	err = table.Create(timeout, storage.NoMetadata, options)
	if err != nil {
		return fmt.Errorf("Error creating table %q in storage account %q: %s", name, storageAccountName, err)
	}

	// TODO: fix the ID
	d.SetId(name)

	return resourceArmStorageTableRead(d, meta)
}

func resourceArmStorageTableRead(d *schema.ResourceData, meta interface{}) error {
	armClient := meta.(*ArmClient)
	ctx := armClient.StopContext

	name := d.Get("name").(string)
	resourceGroupName := d.Get("resource_group_name").(string)
	storageAccountName := d.Get("storage_account_name").(string)

	tableClient, accountExists, err := armClient.getTableServiceClientForStorageAccount(ctx, resourceGroupName, storageAccountName)
	if err != nil {
		return err
	}
	if !accountExists {
		log.Printf("[DEBUG] Storage account %q not found, removing table %q from state", storageAccountName, d.Id())
		d.SetId("")
		return nil
	}

	options := &storage.QueryTablesOptions{}
	tables, err := tableClient.QueryTables(storage.MinimalMetadata, options)
	if err != nil {
		return fmt.Errorf("Failed to retrieve storage tables in account %q: %s", name, err)
	}

	var table *storage.Table
	for _, t := range tables.Tables {
		if t.Name == name {
			table = &t
		}
	}

	if table == nil {
		log.Printf("[INFO] Storage table %q does not exist in account %q, removing from state...", name, storageAccountName)
		d.SetId("")
		return nil
	}

	d.Set("name", table.Name)
	return nil
}

func resourceArmStorageTableDelete(d *schema.ResourceData, meta interface{}) error {
	armClient := meta.(*ArmClient)
	ctx := armClient.StopContext

	name := d.Get("name").(string)
	resourceGroupName := d.Get("resource_group_name").(string)
	storageAccountName := d.Get("storage_account_name").(string)

	tableClient, accountExists, err := armClient.getTableServiceClientForStorageAccount(ctx, resourceGroupName, storageAccountName)
	if err != nil {
		return err
	}
	if !accountExists {
		log.Printf("[INFO] Storage Account %q doesn't exist so the table won't exist", storageAccountName)
		return nil
	}

	table := tableClient.GetTableReference(name)
	timeout := uint(60)
	options := &storage.TableOptions{}

	log.Printf("[INFO] Deleting storage table %q in account %q", name, storageAccountName)
	if err := table.Delete(timeout, options); err != nil {
		return fmt.Errorf("Error deleting storage table %q from storage account %q: %s", name, storageAccountName, err)
	}

	return nil
}

func validateArmStorageTableName(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	if value == "table" {
		errors = append(errors, fmt.Errorf(
			"Table Storage %q cannot use the word `table`: %q",
			k, value))
	}
	if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{2,62}$`).MatchString(value) {
		errors = append(errors, fmt.Errorf(
			"Table Storage %q cannot begin with a numeric character, only alphanumeric characters are allowed and must be between 3 and 63 characters long: %q",
			k, value))
	}

	return
}
