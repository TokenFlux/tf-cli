package cli

import (
	"testing"

	"github.com/tokenflux/tf-cli/internal/config"
)

func TestExplicitLoginNameDoesNotAuthorizeReplacement(t *testing.T) {
	_, creds := fixture(t, map[string][]string{"work": nil})
	c := testCtx()
	if _, err := resolveLoginName(c, creds, "work", true, "sk-new", nil); err == nil {
		t.Fatal("explicit name authorized destructive replacement")
	}
	c.Flags.set["force"] = "true"
	if name, err := resolveLoginName(c, creds, "work", true, "sk-new", nil); err != nil || name != "work" {
		t.Fatalf("%q %v", name, err)
	}
}

func TestSameCredentialDoesNotNeedReplacementConfirmation(t *testing.T) {
	creds := &config.Credentials{Items: map[string]*config.Credential{"work": {Key: "sk-same"}}}
	if err := confirmKeyReplacement(testCtx(), creds, "work", "sk-same"); err != nil {
		t.Fatal(err)
	}
}
