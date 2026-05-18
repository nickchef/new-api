package common

import "strconv"

// Content Moderation Redis key constants.
// All keys are namespaced under "new_api:content_moderation:" to avoid collision
// with other modules.
const (
	contentModerationRedisPrefix = "new_api:content_moderation:"

	// CMRedisKeyFlaggedHashes is the Set storing hashes of inputs that previously
	// hit content moderation. Type: Set.
	CMRedisKeyFlaggedHashes = contentModerationRedisPrefix + "flagged_hashes"

	// CMRedisKeyUserViolationsPrefix is the prefix for per-user violation ZSETs.
	// Use ContentModerationUserViolationsKey(userID) to compose the full key.
	// Type: ZSET (score = unix seconds).
	CMRedisKeyUserViolationsPrefix = contentModerationRedisPrefix + "user_violations:"

	// CMRedisKeyEmailSentPrefix is the prefix for per-user email rate-limit
	// markers. Use ContentModerationEmailSentKey(userID) to compose the full key.
	// Type: String with TTL.
	CMRedisKeyEmailSentPrefix = contentModerationRedisPrefix + "email_sent:"
)

// ContentModerationUserViolationsKey returns the ZSET key for a user's violation window.
func ContentModerationUserViolationsKey(userID int) string {
	return CMRedisKeyUserViolationsPrefix + strconv.Itoa(userID)
}

// ContentModerationEmailSentKey returns the rate-limit marker key for a user.
func ContentModerationEmailSentKey(userID int) string {
	return CMRedisKeyEmailSentPrefix + strconv.Itoa(userID)
}
