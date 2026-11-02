package util

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// GetEthSigningHash hashes a message according to EIP-191.
//
// The data is a UTF-8 encoded string and will envelop as follows:
// `"\x19Ethereum Signed Message:\n" + message.length + message` and hashed
// using keccak256.
func GetEthSigningHash(msg []byte) [32]byte {
	const prefix = "\x19Ethereum Signed Message:\n"
	bytes := []byte(fmt.Sprintf("%v%v", prefix, len(msg)))
	bytes = slices.Concat(
		bytes,
		msg,
	)
	return crypto.Keccak256Hash(bytes)
}

// RecoverEthSignature recovers the public key from a given signature
//
// Signatures follows EIP-155 with a recovery id of 27 or 28
func RecoverEthSignature(hash []byte, sig []byte) (*ecdsa.PublicKey, error) {
	if len(sig) != 65 {
		return nil, errors.New("invalid signature")
	}

	// Only accept a recovery id of 27 or 28. go-ethereum recovers differently
	// with and without cgo for other values (e.g. 31 and 32 are only rejected
	// with cgo), which would make verification non-deterministic across builds.
	if sig[64] != 27 && sig[64] != 28 {
		return nil, errors.New("invalid signature recovery id")
	}

	// Copy so the caller's signature is not modified.
	sig = bytes.Clone(sig)

	// Sub 27 of the recovery id according to this - https://eips.ethereum.org/EIPS/eip-155
	sig[64] -= 27

	recoveredPubKey, err := crypto.SigToPub(hash[:], sig[:])
	if err != nil {
		return nil, err
	}

	return recoveredPubKey, nil
}

func DecodeEthHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")

	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func EncodeEthHex(b []byte) string {
	return fmt.Sprintf("0x%s", hex.EncodeToString(b))
}
