package volume

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/digitalocean/terraform-provider-digitalocean/digitalocean/config"
	"github.com/digitalocean/terraform-provider-digitalocean/digitalocean/util"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/id"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

func ResourceDigitalOceanVolumeAttachment() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceDigitalOceanVolumeAttachmentCreate,
		ReadContext:   resourceDigitalOceanVolumeAttachmentRead,
		DeleteContext: resourceDigitalOceanVolumeAttachmentDelete,

		Schema: map[string]*schema.Schema{
			"droplet_id": {
				Type:         schema.TypeInt,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.NoZeroValues,
			},

			"volume_id": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.NoZeroValues,
			},
		},
	}
}

func resourceDigitalOceanVolumeAttachmentCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*config.CombinedConfig).GodoClient()

	dropletId := d.Get("droplet_id").(int)
	volumeId := d.Get("volume_id").(string)

	volume, _, err := client.Storage.GetVolume(context.Background(), volumeId)
	if err != nil {
		return diag.Errorf("Error retrieving volume: %s", err)
	}

	if len(volume.DropletIDs) == 0 || volume.DropletIDs[0] != dropletId {

		// Only one volume can be attached at one time to a single droplet.
		err := retry.RetryContext(ctx, 5*time.Minute, func() *retry.RetryError {

			log.Printf("[DEBUG] Attaching Volume (%s) to Droplet (%d)", volumeId, dropletId)
			action, _, err := client.StorageActions.Attach(context.Background(), volumeId, dropletId)
			if err != nil {
				if util.IsDigitalOceanError(err, 422, "Droplet already has a pending event.") {
					log.Printf("[DEBUG] Received %s, retrying attaching volume to droplet", err)
					return retry.RetryableError(err)
				}

				return retry.NonRetryableError(
					fmt.Errorf("[WARN] Error attaching volume (%s) to Droplet (%d): %s", volumeId, dropletId, err))
			}

			log.Printf("[DEBUG] Volume attach action id: %d", action.ID)
			if err = util.WaitForAction(client, action); err != nil {
				return retry.NonRetryableError(
					fmt.Errorf("[DEBUG] Error waiting for attach volume (%s) to Droplet (%d) to finish: %s", volumeId, dropletId, err))
			}

			return nil
		})

		if err != nil {
			return diag.Errorf("Error attaching volume to droplet after retry timeout: %s", err)
		}
	}

	d.SetId(id.PrefixedUniqueId(fmt.Sprintf("%d-%s-", dropletId, volumeId)))

	return nil
}

func resourceDigitalOceanVolumeAttachmentRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*config.CombinedConfig).GodoClient()

	dropletId := d.Get("droplet_id").(int)
	volumeId := d.Get("volume_id").(string)

	volume, resp, err := client.Storage.GetVolume(context.Background(), volumeId)
	if err != nil {
		// If the volume is already destroyed, mark as
		// successfully removed
		if resp != nil && resp.StatusCode == 404 {
			d.SetId("")
			return nil
		}

		return diag.Errorf("Error retrieving volume: %s", err)
	}

	if len(volume.DropletIDs) == 0 || volume.DropletIDs[0] != dropletId {
		log.Printf("[DEBUG] Volume Attachment (%s) not found, removing from state", d.Id())
		d.SetId("")
	}

	return nil
}

func resourceDigitalOceanVolumeAttachmentDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*config.CombinedConfig).GodoClient()

	dropletId := d.Get("droplet_id").(int)
	volumeId := d.Get("volume_id").(string)

	// Only one volume can be detached at one time to a single droplet.
	err := retry.RetryContext(ctx, 5*time.Minute, func() *retry.RetryError {

		log.Printf("[DEBUG] Detaching Volume (%s) from Droplet (%d)", volumeId, dropletId)
		action, _, err := client.StorageActions.DetachByDropletID(context.Background(), volumeId, dropletId)
		if err != nil {
			if util.IsDigitalOceanError(err, 422, "Droplet already has a pending event.") {
				log.Printf("[DEBUG] Received %s, retrying detaching volume from droplet", err)
				return retry.RetryableError(err)
			}

			return retry.NonRetryableError(
				fmt.Errorf("[WARN] Error detaching volume (%s) from Droplet (%d): %s", volumeId, dropletId, err))
		}

		log.Printf("[DEBUG] Volume detach action id: %d", action.ID)
		if err = util.WaitForAction(client, action); err != nil {
			return retry.NonRetryableError(
				fmt.Errorf("Error waiting for detach volume (%s) from Droplet (%d) to finish: %s", volumeId, dropletId, err))
		}

		return nil
	})

	if err != nil {
		return diag.Errorf("Error detaching volume from droplet after retry timeout: %s", err)
	}

	return nil
}
