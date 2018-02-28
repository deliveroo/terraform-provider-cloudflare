package cloudflare

import (
	"fmt"
	"log"
	"strings"

	"github.com/cloudflare/cloudflare-go"
	"github.com/hashicorp/terraform/helper/schema"
)

const recordNotFoundMessage = "Invalid dns record identifier"

func resourceCloudFlareRecord() *schema.Resource {
	return &schema.Resource{
		Create:   resourceCloudFlareRecordCreate,
		Read:     resourceCloudFlareRecordRead,
		Update:   resourceCloudFlareRecordUpdate,
		Delete:   resourceCloudFlareRecordDelete,
		Importer: &schema.ResourceImporter{State: importRecord},

		SchemaVersion: 1,
		MigrateState:  resourceCloudFlareRecordMigrateState,
		Schema: map[string]*schema.Schema{
			"domain": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"subdomain": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "",
			},

			"name": {
				Type:       schema.TypeString,
				Optional:   true,
				Deprecated: "Please use 'subdomain' instead",
			},

			"type": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"value": {
				Type:     schema.TypeString,
				Required: true,
			},

			"ttl": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
			},

			"priority": {
				Type:     schema.TypeInt,
				Optional: true,
			},

			"proxied": {
				Default:  false,
				Optional: true,
				Type:     schema.TypeBool,
			},

			"zone_id": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceCloudFlareRecordCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*cloudflare.API)

	subdomain := d.Get("subdomain").(string)
	domain := d.Get("domain").(string)
	newRecord := cloudflare.DNSRecord{
		Type:     d.Get("type").(string),
		Name:     recordName(subdomain, domain),
		Content:  d.Get("value").(string),
		Proxied:  d.Get("proxied").(bool),
		ZoneName: domain,
	}

	if priority, ok := d.GetOk("priority"); ok {
		newRecord.Priority = priority.(int)
	}

	if ttl, ok := d.GetOk("ttl"); ok {
		newRecord.TTL = ttl.(int)
	}

	// Validate value based on type
	if err := validateRecordName(newRecord.Type, newRecord.Content); err != nil {
		return fmt.Errorf("Error validating record name %q: %s", newRecord.Name, err)
	}

	// Validate type
	if err := validateRecordType(newRecord.Type, newRecord.Proxied); err != nil {
		return fmt.Errorf("Error validating record type %q: %s", newRecord.Type, err)
	}

	zoneID, err := client.ZoneIDByName(newRecord.ZoneName)
	if err != nil {
		return fmt.Errorf("Error finding zone %q: %s", newRecord.ZoneName, err)
	}

	d.Set("zone_id", zoneID)
	newRecord.ZoneID = zoneID

	log.Printf("[DEBUG] CloudFlare Record create configuration: %#v", newRecord)

	r, err := client.CreateDNSRecord(zoneID, newRecord)
	if err != nil {
		return fmt.Errorf("Failed to create record: %s", err)
	}

	// In the Event that the API returns an empty DNS Record, we verify that the
	// ID returned is not the default ""
	if r.Result.ID == "" {
		return fmt.Errorf("Failed to find record in Creat response; Record was empty")
	}

	d.SetId(r.Result.ID)

	log.Printf("[INFO] CloudFlare Record ID: %s", d.Id())

	return resourceCloudFlareRecordRead(d, meta)
}

func resourceCloudFlareRecordRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*cloudflare.API)
	domain := d.Get("domain").(string)

	zoneID, err := client.ZoneIDByName(domain)
	if err != nil {
		return fmt.Errorf("Error finding zone %q: %s", domain, err)
	}

	record, err := client.DNSRecord(zoneID, d.Id())
	if err != nil && strings.Contains(err.Error(), recordNotFoundMessage) {
		d.SetId("")
		return nil
	}
	if err != nil {
		return err
	}

	d.SetId(record.ID)
	d.Set("type", record.Type)
	d.Set("subdomain", subdomainName(record.Name, domain))
	d.Set("value", record.Content)
	d.Set("ttl", record.TTL)
	d.Set("priority", record.Priority)
	d.Set("proxied", record.Proxied)
	d.Set("zone_id", zoneID)

	return nil
}

func resourceCloudFlareRecordUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*cloudflare.API)

	subdomain := d.Get("subdomain").(string)
	domain := d.Get("domain").(string)
	updateRecord := cloudflare.DNSRecord{
		ID:       d.Id(),
		Type:     d.Get("type").(string),
		Name:     recordName(subdomain, domain),
		Content:  d.Get("value").(string),
		ZoneName: domain,
		Proxied:  false,
	}

	if priority, ok := d.GetOk("priority"); ok {
		updateRecord.Priority = priority.(int)
	}

	if proxied, ok := d.GetOk("proxied"); ok {
		updateRecord.Proxied = proxied.(bool)
	}

	if ttl, ok := d.GetOk("ttl"); ok {
		updateRecord.TTL = ttl.(int)
	}

	zoneID, err := client.ZoneIDByName(updateRecord.ZoneName)
	if err != nil {
		return fmt.Errorf("Error finding zone %q: %s", updateRecord.ZoneName, err)
	}

	updateRecord.ZoneID = zoneID

	log.Printf("[DEBUG] CloudFlare Record update configuration: %#v", updateRecord)
	err = client.UpdateDNSRecord(zoneID, d.Id(), updateRecord)
	if err != nil {
		return fmt.Errorf("Failed to update CloudFlare Record: %s", err)
	}

	return resourceCloudFlareRecordRead(d, meta)
}

func resourceCloudFlareRecordDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*cloudflare.API)
	domain := d.Get("domain").(string)

	zoneID, err := client.ZoneIDByName(domain)
	if err != nil {
		return fmt.Errorf("Error finding zone %q: %s", domain, err)
	}

	log.Printf("[INFO] Deleting CloudFlare Record: %s, %s", domain, d.Id())

	err = client.DeleteDNSRecord(zoneID, d.Id())
	if err == nil || strings.Contains(err.Error(), recordNotFoundMessage) {
		return nil
	}
	return fmt.Errorf("Error deleting CloudFlare Record: %s", err)
}

func subdomainName(fullName, domain string) string {
	return strings.TrimSuffix(
		strings.TrimSuffix(fullName, domain),
		".",
	)
}

func recordName(subdomain, domain string) string {
	if subdomain == "" {
		return domain
	}
	return fmt.Sprintf("%s.%s", subdomain, domain)
}

func importRecord(d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	client := meta.(*cloudflare.API)
	tokens := strings.Split(d.Id(), "|")
	if len(tokens) != 3 {
		return nil, fmt.Errorf("expecting subdomain|domain|type, got %q", d.Id())
	}
	subdomain, domain, recordType := tokens[0], tokens[1], tokens[2]
	zoneID, err := client.ZoneIDByName(domain)
	if err != nil {
		return nil, fmt.Errorf("error finding zone %q: %s", domain, err)
	}
	filter := cloudflare.DNSRecord{
		Name: fmt.Sprintf("%s.%s", subdomain, domain),
		Type: recordType,
	}
	records, err := client.DNSRecords(zoneID, filter)
	if err != nil {
		return nil, fmt.Errorf("error filtering DNS records: %q", err)
	}
	if len(records) != 1 {
		return nil, fmt.Errorf("expected 1 record, got %d", len(records))
	}
	d.SetId(records[0].ID)
	if err := d.Set("domain", domain); err != nil {
		return nil, fmt.Errorf("error setting domain %v", err)
	}
	if err := resourceCloudFlareRecordRead(d, meta); err != nil {
		return nil, fmt.Errorf("error importing record %q", records[0].ID)
	}
	return []*schema.ResourceData{d}, nil
}
