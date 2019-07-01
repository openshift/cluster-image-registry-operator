package azure

import (
	"regexp"
	"testing"
)

func TestGenerateAccountName(t *testing.T) {
	var re = regexp.MustCompile(`^[0-9a-z]{3,24}$`)
	for _, infrastructureName := range []string{
		"",
		"foo",
		"foo-bar-baz",
		"FOO-BAR-3000",
		"1234567890123456789",
		"123456789012345678901234",
	} {
		accountName := generateAccountName(infrastructureName)
		t.Logf("infrastructureName=%q, accountName=%q", infrastructureName, accountName)
		if !re.MatchString(accountName) {
			t.Errorf("infrastructureName=%q: generated invalid account name: %q", infrastructureName, accountName)
		}
	}
}
