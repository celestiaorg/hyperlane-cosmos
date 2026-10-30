package util_test

import (
	"bytes"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/ethereum/go-ethereum/crypto"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

/*

TEST CASES - eth_signing.go

* Recover (valid) signature with recovery id 27 or 28
* Recover (invalid) signature with non-canonical recovery id
* Recover does not modify the signature

*/

var _ = Describe("eth_signing.go", Ordered, func() {
	key, _ := crypto.ToECDSA(crypto.Keccak256([]byte("eth signing test key")))
	hash := util.GetEthSigningHash([]byte("hello"))

	sign := func() []byte {
		sig, err := crypto.Sign(hash[:], key)
		Expect(err).To(BeNil())
		sig[64] += 27
		return sig
	}

	It("Recover (valid) signature with recovery id 27 or 28", func() {
		// Arrange
		sig := sign()

		// Act
		pubKey, err := util.RecoverEthSignature(hash[:], sig)

		// Assert
		Expect(err).To(BeNil())
		Expect(crypto.PubkeyToAddress(*pubKey)).To(Equal(crypto.PubkeyToAddress(key.PublicKey)))
	})

	It("Recover (invalid) signature with non-canonical recovery id", func() {
		// Results for these values depend on whether go-ethereum is built
		// with cgo, so they must be rejected before recovery.
		for _, delta := range []byte{2, 4, 6, 8, 200, 256 - 24, 256 - 26} {
			// Arrange
			sig := sign()
			sig[64] += delta

			// Act
			_, err := util.RecoverEthSignature(hash[:], sig)

			// Assert
			Expect(err).To(HaveOccurred(), "recovery id %d", sig[64])
		}
	})

	It("Recover does not modify the signature", func() {
		// Arrange
		sig := sign()
		original := bytes.Clone(sig)

		// Act
		_, err := util.RecoverEthSignature(hash[:], sig)

		// Assert
		Expect(err).To(BeNil())
		Expect(sig).To(Equal(original))
	})
})
