package vault

import (
	"github.com/go-resty/resty/v2"
	"elpsykongroo.com/monitor/pkg/types"
	"encoding/json"
	"github.com/sirupsen/logrus"
	"strings"

)
var logger = logrus.New()

func ValidateTotp (code string, config types.Config) bool {
	client := resty.New()

	token := login(config, true)
	resp, err := client.R().
		SetHeader("X-Vault-Token", token).
		Get(config.VaultCloudUri + "/v1/totp/code/" + config.VaultPublicUser)
		logger.Info("totp resp:", resp.String())

	if err != nil {
		logger.Error("get code err:", err)
		return false
	}
	var codeData map[string]interface{}
	err = json.Unmarshal(resp.Body(), &codeData)
	if err != nil {
		return false
	}
	c :=  codeData["data"].(map[string]interface{})["code"].(string)
	if (code == c) {
		return true
	} else {
		return false
	}
}

func ReportIpByCheck (config types.Config) {
	client := resty.New()
	token := login(config, false)
	if token != "" {
		logger.Info ("current config path: {}, key: {} ", config.VaultUri + config.VaultConfigPath, config.VaultCustomKey)
		resp, err := client.R().
		SetHeader("X-Vault-Token", token).
		Get(config.VaultUri + config.VaultConfigPath)

		if err != nil {
			logger.Error("get config err:", err)
			return
		}

		var data map[string]interface{}
		err = json.Unmarshal(resp.Body(), &data)
		if err != nil {
			logger.Error("marshal json err:", err)
			return
		}
		currentConfigKey, ok := data["data"].(map[string]interface{})["data"].(map[string]interface{})[config.VaultCustomKey].(string);
		logger.Info("current config custom kv ", currentConfigKey)

		if !ok {
			logger.Error("get custom key err")
			return
		}

		checkResp, err := client.R().Get(config.IpCheckUrl)
		logger.Info("ip check result ", checkResp.String())
		if !strings.Contains(currentConfigKey, checkResp.String()) {
			data["data"].(map[string]interface{})["data"].(map[string]interface{})[config.VaultCustomKey] =  currentConfigKey + "," + checkResp.String()
			logger.Debug("modify config kv ", data["data"].(map[string]interface{})["data"].(map[string]interface{})[config.VaultCustomKey])
			reportResp, err := client.R().
				SetHeader("X-Vault-Token", token).
				SetBody(data["data"].(map[string]interface{})).
				Post(config.VaultUri + config.VaultConfigPath)
			if err != nil {
				logger.Error("modift config err", err)
			}
			logger.Info("report result:", reportResp.String())
		} else {
			logger.Info("already exist, skip report")
		}
	}	
}

func login(config types.Config, online bool) string {
	client := resty.New()

	body := map[string]string{
		"password": config.Password,
	}
	var serverUri string;
	if online {
		serverUri = config.VaultCloudUri
	} else {
		serverUri = config.VaultUri

	}
	loginResp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(body).Post(serverUri + "/v1/auth/userpass/login/" + config.Username)

	if err != nil {
		logger.Error("login vault err:", err)
		return ""
	}

	var data map[string]interface{}
	err = json.Unmarshal(loginResp.Body(), &data)

	if err != nil {
		logger.Error("Error decoding JSON:", err)
		return ""
	}

	token, ok :=  data["auth"].(map[string]interface{})["client_token"].(string)

	if !ok {
		return ""
	}
	return token
}