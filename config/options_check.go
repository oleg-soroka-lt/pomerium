package config

import (
	"regexp"
)

// KeyAction defines the Pomerium behavior when it encounters a deprecated config field
type KeyAction string

// FieldCheckMsg is a log message to print for a config option
type FieldCheckMsg string

const (
	// KeyActionWarn would result in warning to log
	KeyActionWarn = KeyAction("warn")
	// KeyActionError would result in error in log and possibly program stop
	KeyActionError = KeyAction("error")
	// UnknownFieldAction default behavior when observing an unknown field is to warn
	UnknownFieldAction = KeyActionWarn
	// FieldCheckMsgRemoved log message when field was removed
	FieldCheckMsgRemoved = FieldCheckMsg("config option was removed")
	// FieldCheckMsgUnknown log message for unrecognized / unhandled config option
	FieldCheckMsgUnknown = FieldCheckMsg("unknown config option")
)

var reKeyPath = regexp.MustCompile(`\[\d+\]`)

var (
	// options that were deprecated in the config
	removedConfigFields = map[string]string{
		"idp_service_account":             "https://docs.pomerium.com/docs/overview/upgrading#idp-directory-sync",
		"idp_refresh_directory_timeout":   "https://docs.pomerium.com/docs/overview/upgrading#idp-directory-sync",
		"idp_refresh_directory_interval":  "https://docs.pomerium.com/docs/overview/upgrading#idp-directory-sync",
		"idp_qps":                         "https://docs.pomerium.com/docs/overview/upgrading#idp-directory-sync",
		"routes.allowed_groups":           "https://docs.pomerium.com/docs/overview/upgrading#idp-groups-policy",
		"routes.set_authorization_header": "https://www.pomerium.com/docs/deploy/core/upgrading#set-authorization-header",
	}

	// mapstructure has issues with embedded protobuf structs that we should ignore
	ignoreConfigFields = map[string]struct{}{
		"routes.outlier_detection": {},
		"routes.health_checks":     {},
	}
)

// FieldMsg returns information
type FieldMsg struct {
	Key     string
	DocsURL string
	FieldCheckMsg
	KeyAction
}

// CheckUnknownConfigFields returns list of messages to be emitted about unrecognized fields
func CheckUnknownConfigFields(fields []string) []FieldMsg {
	out := make([]FieldMsg, 0, len(fields))

	for _, key := range fields {
		path := reKeyPath.ReplaceAllString(key, "")

		if docsURL, ok := removedConfigFields[path]; ok {
			out = append(out, FieldMsg{
				Key:           path,
				DocsURL:       docsURL,
				KeyAction:     KeyActionError,
				FieldCheckMsg: FieldCheckMsgRemoved,
			})
			continue
		}

		if _, ok := ignoreConfigFields[path]; ok {
			continue
		}

		out = append(out, FieldMsg{
			Key:           path,
			KeyAction:     KeyActionWarn,
			FieldCheckMsg: FieldCheckMsgUnknown,
		})
	}

	return out
}
