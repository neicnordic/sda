package helper

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"golang.org/x/crypto/ssh"
)

// Global variables for test token creation
var (
	DefaultTokenClaims = map[string]interface{}{
		"iss":   "https://dummy.ega.nbis.se",
		"sub":   "dummy",
		"exp":   time.Now().Add(time.Hour * 2).Unix(),
		"pilot": "dummy-pilot",
	}

	WrongUserClaims = map[string]interface{}{
		"sub":   "c5773f41d17d27bd53b1e6794aedc32d7906e779@elixir-europe.org",
		"aud":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"azp":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"scope": "ga4gh_passport_v1 openid",
		"iss":   "https://dummy.ega.nbis.se",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour * 2).Unix(),
		"jti":   "5e6c6d24-42eb-408e-ba20-203904e388a1",
	}

	ExpiredClaims = map[string]interface{}{
		"sub":   "dummy",
		"aud":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"azp":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"scope": "ga4gh_passport_v1 openid",
		"iss":   "https://dummy.ega.nbis.se",
		"exp":   time.Now().Add(-time.Hour * 2).Unix(),
		"iat":   time.Now().Unix(),
		"jti":   "5e6c6d24-42eb-408e-ba20-203904e388a1",
	}

	NonValidClaims = map[string]interface{}{
		"sub":   "c5773f41d17d27bd53b1e6794aedc32d7906e779@elixir-europe.org",
		"aud":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"azp":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"scope": "ga4gh_passport_v1 openid",
		"iss":   "https://dummy.ega.nbis.se",
		"exp":   time.Now().Add(time.Hour * 2).Unix(),
		"iat":   time.Now().Add(time.Hour * 2).Unix(),
		"jti":   "5e6c6d24-42eb-408e-ba20-203904e388a1",
	}

	WrongTokenAlgClaims = map[string]interface{}{
		"iss":       "Online JWT Builder",
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(time.Hour * 2).Unix(),
		"aud":       "4e9416a7-3515-447a-b848-d4ac7a57f",
		"sub":       "pleasefix@snurre-in-the-house.org",
		"auth_time": "1632207224",
		"jti":       "cc847f9c-7608-4b4f-9c6f-6e734813355f",
	}
)

// Authenticating functionality for testing
// AlwaysAllow is an Authenticator that always authenticates
type AlwaysAllow struct{}

// NewAlwaysAllow returns a new AlwaysAllow authenticator.
func NewAlwaysAllow() *AlwaysAllow {
	return &AlwaysAllow{}
}

// Authenticate authenticates everyone.
func (u *AlwaysAllow) Authenticate(_ *http.Request) (jwt.Token, error) {
	return jwt.New(), nil
}

// AlwaysAllow is an Authenticator that always authenticates
type AlwaysDeny struct{}

// Authenticate does not authenticate anyone.
func (u *AlwaysDeny) Authenticate(_ *http.Request) (jwt.Token, error) {
	return nil, fmt.Errorf("denied")
}

// MakeFolder creates a folder and subfolders for the keys pair
// Returns the two paths
func MakeFolder(path string) (string, string, error) {
	prKeyPath := path + "/private-key"
	pubKeyPath := path + "/public-key"
	err := os.MkdirAll(prKeyPath, 0750)
	if err != nil {
		// fmt.Errorf("error creating directory: %v", err)
		return " ", " ", err
	}
	err = os.MkdirAll(pubKeyPath, 0750)
	if err != nil {
		// fmt.Errorf("error creatin directory: %w", err)
		return " ", " ", err
	}

	return prKeyPath, pubKeyPath, nil
}

// ParsePrivateRSAKey reads and parses the RSA private key
func ParsePrivateRSAKey(path, keyName string) (jwk.Key, error) {
	keyPath := path + keyName
	prKey, err := os.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		return nil, err
	}

	prKeyParsed, err := jwk.ParseKey(prKey, jwk.WithPEM(true))
	if err != nil {
		return nil, err
	}

	if prKeyParsed.KeyType() != "RSA" {
		return nil, fmt.Errorf("bad key format, expected RSA got %v", prKeyParsed.KeyType())
	}

	return prKeyParsed, nil
}

// CreateRSAkeys creates the RSA key pair
func CreateRSAkeys(prPath, pubPath string) error {
	privatekey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	publickey := &privatekey.PublicKey

	// dump private key to file
	privateKeyBytes, err := jwk.EncodePEM(privatekey)
	if err != nil {
		return err
	}
	_ = os.WriteFile(prPath+"/rsa", privateKeyBytes, 0600)

	// dump public key to file
	publicKeyBytes, err := jwk.EncodePEM(publickey)
	if err != nil {
		return err
	}
	_ = os.WriteFile(pubPath+"/rsa.pub", publicKeyBytes, 0600)

	return nil
}

func CreateSSHKey(path string) error {
	privatekey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	pk := &privatekey.PublicKey

	// dump private key to file
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privatekey)
	privateKeyBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}
	encPrivateKeyBlock, err := x509.EncryptPEMBlock(rand.Reader, privateKeyBlock.Type, privateKeyBlock.Bytes, []byte("password"), x509.PEMCipherAES256) //nolint:staticcheck
	if err != nil {
		return err
	}
	privatePem, err := os.Create(path + "/id_rsa")
	if err != nil {
		return err
	}
	err = pem.Encode(privatePem, encPrivateKeyBlock)
	if err != nil {
		return err
	}

	err = os.Chmod(path+"/id_rsa", 0600)
	if err != nil {
		return err
	}

	publicKey, err := ssh.NewPublicKey(pk)
	if err != nil {
		return err
	}
	pubKeyBytes := ssh.MarshalAuthorizedKey(publicKey)
	err = os.WriteFile(path+"/id_rsa.pub", pubKeyBytes, 0600)
	if err != nil {
		return err
	}

	return nil
}

// CreateRSAToken creates an RSA token
func CreateRSAToken(jwtKey jwk.Key, headerAlg string, tokenClaims map[string]interface{}) (string, error) {
	if err := jwk.AssignKeyID(jwtKey); err != nil {
		return "AssignKeyID failed", err
	}
	if err := jwtKey.Set(jwk.AlgorithmKey, jwa.KeyAlgorithmFrom(headerAlg)); err != nil {
		return "Set algorithm failed", err
	}

	token := jwt.New()
	for key, value := range tokenClaims {
		if err := token.Set(key, value); err != nil {
			return "failed to set claim", err
		}
	}

	tokenString, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, jwtKey))
	if err != nil {
		return "no-token", err
	}

	return string(tokenString), nil
}

// CreateECToken creates an EC token
func CreateECToken(jwtKey jwk.Key, headerAlg string, tokenClaims map[string]interface{}) (string, error) {
	if err := jwk.AssignKeyID(jwtKey); err != nil {
		return "AssignKeyID failed", err
	}
	if err := jwtKey.Set(jwk.AlgorithmKey, jwa.KeyAlgorithmFrom(headerAlg)); err != nil {
		return "Set algorithm failed", err
	}

	token := jwt.New()
	for key, value := range tokenClaims {
		if err := token.Set(key, value); err != nil {
			return "failed to set claim", err
		}
	}

	tokenString, err := jwt.Sign(token, jwt.WithKey(jwa.ES256, jwtKey))
	if err != nil {
		return "no-token", err
	}

	return string(tokenString), nil
}

// CreateHSToken creates an HS token
func CreateHSToken(key []byte, tokenClaims map[string]interface{}) (string, error) {
	token := jwt.New()
	for key, value := range tokenClaims {
		if err := token.Set(key, value); err != nil {
			return "failed to set claim", err
		}

	}

	jwtKey, err := jwk.FromRaw(key)
	if err != nil {
		return "Create key failed", err
	}
	if err := jwk.AssignKeyID(jwtKey); err != nil {
		return "AssignKeyID failed", err
	}
	tokenString, err := jwt.Sign(token, jwt.WithKey(jwa.HS256, jwtKey))
	if err != nil {
		return "no-token", err
	}

	return string(tokenString), nil
}

// ParsePrivateECKey reads and parses the EC private key
func ParsePrivateECKey(path, keyName string) (jwk.Key, error) {
	keyPath := path + keyName
	prKey, err := os.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		return nil, err
	}

	prKeyParsed, err := jwk.ParseKey(prKey, jwk.WithPEM(true))
	if err != nil {
		return nil, err
	}

	if prKeyParsed.KeyType() != "EC" {
		return nil, fmt.Errorf("bad key format, expected EC got %v", prKeyParsed.KeyType())
	}

	return prKeyParsed, nil
}

// CreateECkeys creates the EC key pair
func CreateECkeys(prPath, pubPath string) error {
	privatekey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	publickey := &privatekey.PublicKey

	// dump private key to file
	privateKeyBytes, err := jwk.EncodePEM(privatekey)
	if err != nil {
		return err
	}
	_ = os.WriteFile(prPath+"/ec", privateKeyBytes, 0600)

	// dump public key to file
	publicKeyBytes, err := jwk.EncodePEM(publickey)
	if err != nil {
		return err
	}
	_ = os.WriteFile(pubPath+"/ec.pub", publicKeyBytes, 0600)

	return nil
}

func MakeCerts(outDir string) {
	// set up our CA certificate
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2000),
		Subject: pkix.Name{
			Organization: []string{"NEIC"},
			CommonName:   "Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, 1),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// create our private and public key
	caPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalln(err)
	}

	// create the CA certificate
	caBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		log.Fatalln(err)
	}

	err = TLScertToFile(outDir+"/ca.crt", caBytes)
	if err != nil {
		log.Fatalln(err)
	}

	tlsKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalln(err)
	}

	err = TLSkeyToFile(outDir+"/tls.key", tlsKey)
	if err != nil {
		log.Fatalln(err)
	}

	// set up our server certificate
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2121),
		Subject: pkix.Name{
			Organization: []string{"NEIC"},
			CommonName:   "test_cert",
		},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:              []string{"localhost", "mq", "proxy", "s3"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, 1),
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IsCA:                  false,
		BasicConstraintsValid: true,
	}

	// create the TLS certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, certTemplate, caTemplate, &tlsKey.PublicKey, caPrivKey)
	if err != nil {
		log.Fatalln(err)
	}

	err = TLScertToFile(outDir+"/tls.crt", certBytes)
	if err != nil {
		log.Fatalln(err)
	}
	log.Printf("certificartes written to: %s", outDir)
}

func TLSkeyToFile(filename string, key *ecdsa.PrivateKey) error {
	keyFile, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer keyFile.Close()

	pk, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	err = pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: pk})

	return err
}

func TLScertToFile(filename string, derBytes []byte) error {
	certFile, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer certFile.Close()
	err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	return err
}

func AnonymizeFilepath(filepath string, username string) string {
	return strings.ReplaceAll(filepath, strings.Replace(username, "@", "_", 1)+"/", "")
}

func UnanonymizeFilepath(filepath string, username string) string {
	return strings.Replace(username, "@", "_", 1) + "/" + filepath
}
