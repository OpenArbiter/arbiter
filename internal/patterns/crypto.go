package patterns

var CryptoOperations = Category{
	Name:        "crypto_operations",
	Description: "Cryptographic operations (including weak/insecure usage)",
	Patterns: []string{
		"crypto/", "hashlib", "bcrypt", "jwt.",
		"PrivateKey", "PublicKey", "x509",
		// Weak crypto patterns
		"hashlib.md5", "hashlib.sha1",
		"MD5.Create", "SHA1.Create",
		"MODE_ECB", "AES.MODE_ECB",
		"DES.new", "DESede",
	},
}
