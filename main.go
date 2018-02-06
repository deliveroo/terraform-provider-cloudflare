package main

import (
	"github.com/deliveroo/terraform-provider-cloudflare/cloudflare"
	"github.com/hashicorp/terraform/plugin"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: cloudflare.Provider,
	})
}
