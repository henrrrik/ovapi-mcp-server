package tools

import (
	"strconv"
	"strings"
)

// journeyIDForOperator rewrites an upstream journey key so /journey/{id}
// resolves it. Upstream keys passes and line actuals by DataOwnerCode
// ("NL_20260922_7_10926_0") while the journey endpoint only knows
// OperatorCode-prefixed ids ("GVB_20260922_7_10926_0"). When the two codes
// differ and the key carries the owner prefix, the prefix is swapped;
// otherwise the key is returned unchanged.
func journeyIDForOperator(id, ownerCode, operatorCode string) string {
	if ownerCode == "" || operatorCode == "" || ownerCode == operatorCode {
		return id
	}
	if !strings.HasPrefix(id, ownerCode+"_") {
		return id
	}
	return operatorCode + id[len(ownerCode):]
}

// lineIDFor builds the id /line/{id} resolves: the operator code (falling
// back to the data owner), the line planning number (not the public number)
// and the direction. Live, every id in the /line index is data-owner
// prefixed ("NL_5302_1") and none of those resolve, so this is the only
// reliable handoff from a departure or journey to the lines tool.
func lineIDFor(operatorCode, ownerCode, planningNumber string, direction int) string {
	prefix := operatorCode
	if prefix == "" {
		prefix = ownerCode
	}
	if prefix == "" || planningNumber == "" || direction == 0 {
		return ""
	}
	return prefix + "_" + planningNumber + "_" + strconv.Itoa(direction)
}

// normalizeUpstreamTime renders any upstream timestamp (offset-less
// Amsterdam wall clock, or a UTC "Z" server clock) with the Europe/Amsterdam
// offset, so every time in a lean shape is directly comparable. Unparseable
// input is returned as is.
func normalizeUpstreamTime(s string) string {
	t := parseAmsterdamTime(s)
	if t.IsZero() {
		return s
	}
	return formatWithOffset(t)
}
