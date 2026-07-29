package postgres

import (
	"os"
	"strings"
	"testing"
)

// queryFiles maps each embedded statement to the file it came from. go:embed
// already turns a missing file into a build error, so what is left to catch is
// a file that exists but is empty, and a file that is added to queries/ and
// never wired to a var.
var queryFiles = map[string]string{
	"account_find_by_phone.sql":                  qAccountFindByPhone,
	"account_find_by_id.sql":                     qAccountFindByID,
	"account_find_by_google_subject.sql":         qAccountFindByGoogleSubject,
	"account_list.sql":                           qAccountList,
	"account_count_created_since.sql":            qAccountCountCreatedSince,
	"account_create.sql":                         qAccountCreate,
	"verification_create.sql":                    qVerificationCreate,
	"verification_get.sql":                       qVerificationGet,
	"verification_find_latest_by_phone.sql":      qVerificationFindLatestByPhone,
	"verification_locked_phones.sql":             qVerificationLockedPhones,
	"verification_count_sends_since.sql":         qVerificationCountSendsSince,
	"verification_sum_failed_attempts_since.sql": qVerificationSumFailedAttemptsSince,
	"verification_count_by_status_since.sql":     qVerificationCountByStatusSince,
	"verification_update.sql":                    qVerificationUpdate,
	"session_create.sql":                         qSessionCreate,
	"session_find_by_token_hash.sql":             qSessionFindByTokenHash,
	"session_update.sql":                         qSessionUpdate,
	"session_revoke_by_token_hash.sql":           qSessionRevokeByTokenHash,
	"audit_event_create.sql":                     qAuditEventCreate,
}

func TestEmbeddedQueriesAreNotEmpty(t *testing.T) {
	verbs := []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
	for name, q := range queryFiles {
		if strings.TrimSpace(q) == "" {
			t.Errorf("%s embedded as an empty string", name)
			continue
		}
		upper := strings.ToUpper(q)
		if !slicesContainsAny(upper, verbs) {
			t.Errorf("%s contains no statement verb:\n%s", name, q)
		}
	}
}

// A .sql file nobody embeds is dead weight that reads as a live query.
func TestEveryQueryFileIsEmbedded(t *testing.T) {
	entries, err := os.ReadDir("queries")
	if err != nil {
		t.Fatalf("read queries dir: %v", err)
	}

	onDisk := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			onDisk[e.Name()] = true
		}
	}

	for name := range queryFiles {
		if !onDisk[name] {
			t.Errorf("%s is embedded but missing from queries/", name)
		}
		delete(onDisk, name)
	}
	for name := range onDisk {
		t.Errorf("queries/%s exists but no var embeds it", name)
	}
}

func slicesContainsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
