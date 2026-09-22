package tools

import "strings"

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
