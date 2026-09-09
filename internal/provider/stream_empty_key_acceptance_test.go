package provider_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccStream_rejectsEmptyKeyRotation(t *testing.T) {
	suffix := testAccSuffix()
	key := "old-secret-" + suffix
	config := testAccStreamConfig(t, suffix, "empty_key_"+suffix, key, "Key rotation")
	empty := strings.Replace(config, fmt.Sprintf("plaintext = %q", key), `plaintext = ""`, 1)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testAccProtoV6ProviderFactories, CheckDestroy: testAccCheckDestroyRemote, Steps: []resource.TestStep{
		{Config: config},
		{Config: empty, ExpectError: regexp.MustCompile("plaintext must not be empty")},
		{Config: config, PlanOnly: true},
	}})
}
