# monitor

```json
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
    "EnableQuery": false, // true/false
    "CheckDuration": 100 // second
    "UploadDuration": 5 // minute
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
