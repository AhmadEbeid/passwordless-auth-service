package postgres

import _ "embed"

// Every statement this adapter runs lives in queries/ as a standalone .sql
// file, embedded here into a string. Embedding into a string rather than an
// embed.FS keeps the compile-time guarantee an inline const had: a renamed or
// missing file fails the build, not a request. Keeping the SQL in .sql files
// buys editor support, real diffs, and the ability to paste a statement
// straight into psql or EXPLAIN without unpicking Go quoting.

//go:embed queries/account_find_by_phone.sql
var qAccountFindByPhone string

//go:embed queries/account_find_by_id.sql
var qAccountFindByID string

//go:embed queries/account_find_by_google_subject.sql
var qAccountFindByGoogleSubject string

//go:embed queries/account_list.sql
var qAccountList string

//go:embed queries/account_count_created_since.sql
var qAccountCountCreatedSince string

//go:embed queries/account_create.sql
var qAccountCreate string

//go:embed queries/verification_create.sql
var qVerificationCreate string

//go:embed queries/verification_get.sql
var qVerificationGet string

//go:embed queries/verification_find_latest_by_phone.sql
var qVerificationFindLatestByPhone string

//go:embed queries/verification_locked_phones.sql
var qVerificationLockedPhones string

//go:embed queries/verification_count_sends_since.sql
var qVerificationCountSendsSince string

//go:embed queries/verification_sum_failed_attempts_since.sql
var qVerificationSumFailedAttemptsSince string

//go:embed queries/verification_count_by_status_since.sql
var qVerificationCountByStatusSince string

//go:embed queries/verification_update.sql
var qVerificationUpdate string

//go:embed queries/session_create.sql
var qSessionCreate string

//go:embed queries/session_find_by_token_hash.sql
var qSessionFindByTokenHash string

//go:embed queries/session_update.sql
var qSessionUpdate string

//go:embed queries/session_revoke_by_token_hash.sql
var qSessionRevokeByTokenHash string

//go:embed queries/audit_event_create.sql
var qAuditEventCreate string
