# monitor

```json
{
    "endpoint": "", // s3 config
    "bucket": "", // s3 config
    "region": "", // s3 config
    "monitorUrl": "", // which url can return statuscode normally
    "ipCheckUrl": "",
    "name": "monitor", // any
    "introspectUrl": "", // oauth2 config
    "clientId": "", // oauth2 config
    "clientSecret": "", // oauth2 config
    "enableCheck": false, // true/false
    "enableIpCheck": false, // true/false
    "enableQuery": false, // true/false
    "enableUpload": false, // true/false
    "enableSync": false, // true/false
    "enableWol": false, // true/false
    "forceSync": false,// true/false if set false, only fetch data which not exist local (recommend), true will check all data sha256
    "checkDuration": 100, // second
    "uploadDuration": 5, // minute
    "syncDuration": 100, // minute
    "reportDuration": 6,// hour
    "password": "", //vault userpass passwd
    "username": "", //vault userpass user
    "vaultUri": "", //vault uri
    "vaultConfigPath": "" ,//vault kv path
    "vaultCustomKey": ""//vault config update key
}
```

```text
config.json need create in homeDir/.aws work with aws sdk

aws-sdk config 

credentials:
[default]
aws_access_key_id = ""
aws_secret_access_key = ""

or env 
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY

file struct like this
/home/someone/.aws/credentials
/home/someone/.aws/config.json
```
