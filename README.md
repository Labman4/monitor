# monitor

```json
config.json with s3 and oauth2
{
    "endpoint": "", // s3 config
    "bucket": "", // s3 config
    "region": "", // s3 config
    "monitorUrl": "", // which url can return statuscode normally
    "name": "monitor", // any
    "introspectUrl": "", // oauth2 config
    "clientId": "", // oauth2 config
    "clientSecret": "", // oauth2 config
    "EnableCheck": false, // true/false
    "CheckDuration": 100 // second
    "UploadDuration": 1 // hour

}
```

```text
config.json need create in homeDir/.aws work with aws credentials
credentials struct
[default]
aws_access_key_id = ""
aws_secret_access_key = ""

file struct like this
/home/someone/.aws/credentials
/home/someone/.aws/config.json
```
