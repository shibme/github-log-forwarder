# GitHub Log Forwarder

A log forwarding service that pulls logs from GitHub and pushes to the specified endpoint

### Preparing the environemnt

Set the following environment variables while runnng the container as shown below

- A GitHub token with scope `admin:enterprise`
```
export GHLF_GITHUB_ENTERPRISE_ADMIN_TOKEN="ghp_XXXXXXXXXXXXXXXXXXXX"
```
- Your GitHub enterprise name or ID
```
export GHLF_GITHUB_ENTERPRISE_ID="your_enterprise_name_or_id"
```
- The remote logging endpoint where logs have to be sent to
```
export GHLF_LOGGING_ENDPOINT_URL="https://logging-endpoint.example.com"
```
- The auth header that needs to be set (if required)
```
export GHLF_LOGGING_ENDPOINT_AUTH_HEADER="Bearer 23cSD7SrB7cbhdr5C"
```
- The response code to expect when posting logs. Will throw an error if the response code is different from what is set here
```
export GHLF_LOGGING_ENDPOINT_EXPECTED_RESPONSE_CODE=200
```
- Time interval (in seconds) to wait for if there are no new logs. If not set, will stop when there are no new logs
```
export GHLF_PROCESSING_INTERVAL=60
```

### Getting started

Create a directory to persist app data and run the following command to get started
```
mkdir ghlf
docker run \
    -e GHLF_GITHUB_ENTERPRISE_ADMIN_TOKEN \
    -e GHLF_GITHUB_ENTERPRISE_ID \
    -e GHLF_LOGGING_ENDPOINT_URL \
    -e GHLF_LOGGING_ENDPOINT_AUTH_HEADER \
    -e GHLF_LOGGING_ENDPOINT_EXPECTED_RESPONSE_CODE \
    -v $(pwd)/ghlf/:/data/ \
    shibme/github-log-forwarder
```