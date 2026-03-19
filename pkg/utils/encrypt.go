package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
)

// RSAEncrypt 使用指定的公钥字符串(Base64形式的密钥体)对输入文本进行 PKCS1v15 加密
// 这对标了 Python 脚本中的 rsa_encrypt 逻辑。
func RSAEncrypt(plainText string, pubKeyStr string) (string, error) {
	// 将外部传入的纯文本密钥自动补全为标准的 PEM 格式
	pemBlock := "-----BEGIN PUBLIC KEY-----\n" + pubKeyStr + "\n-----END PUBLIC KEY-----"
	block, _ := pem.Decode([]byte(pemBlock))
	if block == nil {
		return "", errors.New("failed to parse PEM block containing the public key")
	}

	// 转换为通用的 PKIX 数据结构
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}

	// 验证并转型为 RSA 公钥
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("not an RSA public key")
	}

	// 执行 PKCS1_v1_5 模式加密
	cipherText, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, []byte(plainText))
	if err != nil {
		return "", err
	}

	// 返回加密后字节流的 Base64 编码结果
	return base64.StdEncoding.EncodeToString(cipherText), nil
}
