package engine

import "time"

const (
	DefaultReconcileInterval = 5 * time.Minute
	ConditionSynced          = "Synced"
	ConditionReady           = "Ready"
	ReasonSynced             = "Synced"
	ReasonSyncFailed         = "SyncFailed"
	ReasonAppUnreachable     = "AppUnreachable"
	ReasonSecretNotFound     = "SecretNotFound"
	ReasonInvalidConfig      = "InvalidConfig"
)
