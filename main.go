package main

import (
	"context"
	"log"

	"github.com/chilipiper/terraform-provider-jitsu/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

func main() {
	if err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{
		Address: "registry.terraform.io/chilipiper/jitsu",
	}); err != nil {
		log.Fatal(err)
	}
}
