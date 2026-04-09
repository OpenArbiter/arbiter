package patterns

var CryptoOperations = Category{
	Name:        "crypto_operations",
	Description: "Cryptographic operations",
	Patterns: []string{
		"crypto/", "hashlib", "bcrypt", "jwt.",
		"PrivateKey", "PublicKey", "x509",
	},
}
