package beexar

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// SignatureHeader is the header carrying the request signature: hex-encoded
// HMAC-SHA256 of the RAW request body, keyed with the operator's API secret.
// 64 lowercase hex characters.
const SignatureHeader = "X-REQUEST-SIGN"

// Sign returns the signature for the exact bytes you are about to put on the
// wire.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify checks a signature in constant time.
//
// body must be the bytes as received. If anything between the socket and this
// call parsed the JSON and serialised it again, those bytes are gone and no
// amount of care here can recover them.
func Verify(body []byte, signature, secret string) bool {
	if signature == "" {
		return false
	}
	return hmac.Equal([]byte(signature), []byte(Sign(body, secret)))
}
