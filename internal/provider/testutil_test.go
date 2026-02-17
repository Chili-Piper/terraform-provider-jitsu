package provider_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/chilipiper/terraform-provider-jitsu/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"jitsu": providerserver.NewProtocol6WithError(provider.New("test")()),
}

func testAccProviderConfig(t *testing.T) string {
	t.Helper()

	consoleURL := os.Getenv("JITSU_CONSOLE_URL")
	if consoleURL == "" {
		consoleURL = "http://localhost:3300"
	}

	databaseURL := os.Getenv("JITSU_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://reporting:plz_no_hack!@localhost:5432/reporting?sslmode=disable"
	}

	username := os.Getenv("JITSU_USERNAME")
	if username == "" {
		username = "admin@jitsu.com"
	}

	password := os.Getenv("JITSU_PASSWORD")
	if password == "" {
		password = "admin123"
	}

	return fmt.Sprintf(`
provider "jitsu" {
  console_url  = %q
  username     = %q
  password     = %q
  database_url = %q
}
`, consoleURL, username, password, databaseURL)
}

func testAccSuffix() string {
	return strings.ToLower(acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum))
}

func testAccWorkspaceName(prefix, suffix string) string {
	return fmt.Sprintf("%s %s", prefix, suffix)
}

func testAccWorkspaceSlug(prefix, suffix string) string {
	return fmt.Sprintf("%s-%s", prefix, suffix)
}
