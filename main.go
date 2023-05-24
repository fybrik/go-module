package main

import (
	"errors"
	"context"
	"log"
	"io"
	"net/http"
	"encoding/json"
	"os"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
	"github.com/rs/zerolog"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/gin-gonic/gin"
)

type VaultClient struct {
	Address     string
	AuthPath    string
	Role        string
	JwtFilePath string
	Logger      *zerolog.Logger
}

type ConfValues struct {
	Data []struct {
		Name       string `yaml:"name"`
		Capability string `yaml:"capability"`
		Format     string `yaml:"format"`
		Connection struct {
			Type string `yaml:"type"`
			S3   struct {
				Bucket    string `yaml:"bucket"`
				ObjectKey string `yaml:"object_key"`
				EndpointURL      string `yaml:"endpoint_url"`
				VaultCredentials struct {
					Address    string `yaml:"address"`
					AuthPath   string `yaml:"authPath"`
					Role       string `yaml:"role"`
					SecretPath string `yaml:"secretPath"`
				} `yaml:"vault_credentials"`
			} `yaml:"s3"`
			
		} `yaml:"connection"`
		Transformations string `yaml:"transformations"`
	} `yaml:"data"`
}

var confValues ConfValues

func (v *VaultClient) GetToken() (string, error) {
	jwt, err := os.ReadFile(v.JwtFilePath)
	if err != nil {
		v.Logger.Error().Msg("Failed to read JWT file")
		return "", err
	}

	j := make(map[string]string)
	j["jwt"] = string(jwt)
	j["role"] = v.Role

	fullAuthPath := v.Address + v.AuthPath
	jsonStr, err := json.Marshal(j)
	if err != nil {
		v.Logger.Error().Msg("Failed to transform map to JSON string")
		return "", err
	}

	requestBody := strings.NewReader(string(jsonStr))
	resp, _ := http.Post(fullAuthPath, "encoding/json", requestBody) //nolint
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		v.Logger.Error().Msg("Failed to get token from vault")
		return "", err
	}

	responseMap := make(map[string]interface{})
	err = json.Unmarshal(responseBody, &responseMap)
	if err != nil {
		v.Logger.Error().Msg("malformed response from vault")
		return "", err
	}

	var token string
	if value, ok := responseMap["auth"]; ok {
		token = value.(map[string]interface{})["client_token"].(string)
		v.Logger.Info().Msg("Successfully obtained token from Vault")
		return token, nil
	}
	const MalformedVaultResponseMessage = "Malformed response from vault"
	v.Logger.Error().Msg(MalformedVaultResponseMessage)
	return "", errors.New(MalformedVaultResponseMessage)
}

func (v *VaultClient) GetSecret(token, secretPath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), "GET", v.Address+secretPath, http.NoBody)
	if err != nil {
		v.Logger.Error().Msg("Failed to prepare Vault secret request")
		return nil, err
	}

	req.Header.Set("X-Vault-Token", token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		v.Logger.Error().Msg("Failed to obtain secret from Vault")
		return nil, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		v.Logger.Error().Msg("Failed to read Vault secret")
		return nil, err
	}

	v.Logger.Info().Msg("Successfully read Vault secret")
	return responseBody, nil
}



func (v *VaultClient) ExtractS3CredentialsFromSecret(secret []byte) (string, string, error) {
	secretMap := make(map[string]interface{})
	err := json.Unmarshal(secret, &secretMap)
	if err != nil {
		v.Logger.Error().Msg("Malformed secret response from vault")
		return "", "", err
	}

	if value, ok := secretMap["data"]; ok {
		data := value.(map[string]interface{})
		v.Logger.Info().Msg("Successfully extracted S3 credentials from Vault secret")
		return data["access_key"].(string), data["secret_key"].(string), nil
	}
	const FailedToExtractS3CredentialsFromVaultSecret = "Failed to extract S3 credentials from Vault secret"
	v.Logger.Error().Msg(FailedToExtractS3CredentialsFromVaultSecret)
	return "", "", errors.New(FailedToExtractS3CredentialsFromVaultSecret)
}

func setupRouter() *gin.Engine {
	router := gin.Default()
	router.UseRawPath=true 
	router.GET(":key", GetDataAsset)
	return router
}

func GetDataAsset(c *gin.Context) {
	key := c.Param("key")
	found:= false
	for _, data := range confValues.Data {
		if data.Name != key {
			continue
		}
		found = true
		client := VaultClient{
			Address:     data.Connection.S3.VaultCredentials.Address,
			AuthPath:    data.Connection.S3.VaultCredentials.AuthPath,
			Role:        data.Connection.S3.VaultCredentials.Role,
			JwtFilePath: "/var/run/secrets/kubernetes.io/serviceaccount/token",
			Logger:      &zerolog.Logger{},
		}
		token, err := client.GetToken()
		if err != nil {
			client.Logger.Err(err).Msg("Failed to get token from vault")
		}
		secret, err := client.GetSecret(token, data.Connection.S3.VaultCredentials.SecretPath)
		if err != nil {
			client.Logger.Err(err).Msg("Failed to get secret from vault")
		}
		accessKey, secretKey, err := client.ExtractS3CredentialsFromSecret(secret)
		if err != nil {
			client.Logger.Err(err).Msg("Failed to extract S3 credentials from Vault secret")
		}
		useSSL := false
		endpoint :=  data.Connection.S3.EndpointURL
		minioClient, err := minio.New(endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
			Secure: useSSL,
		})
		if err != nil {
			client.Logger.Err(err).Msg("Failed to connect to S3 using Minio client")
		}
		localFile := "./tmp/" + data.Connection.S3.ObjectKey
		if err := minioClient.FGetObject(context.Background(), data.Connection.S3.Bucket, data.Connection.S3.ObjectKey,
			localFile, minio.GetObjectOptions{}); err != nil {
			client.Logger.Err(err).Msg("Failed to fetch object from S3")
		}
		c.File(localFile)
		return
	}
	if found == false {
		var NotFound string = "Data asset: " + key + " not found"
		c.JSON(http.StatusNotFound, gin.H{
			"error": NotFound,
		})
	}
}

func main() {
	confYamlFile, err := os.ReadFile("./etc/conf/conf.yaml")
	if err != nil {
		log.Fatalf("error reading YAML file: %v", err)
	}
	err = yaml.Unmarshal(confYamlFile,&confValues)
	if err != nil {
		log.Fatalf("error unmarshaling YAML data: %v", err)
	}
	router := setupRouter()
	err = router.Run(":8080")
	if err != nil {
		fmt.Println(err)
	}
}
