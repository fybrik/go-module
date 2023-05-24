# Go-module

This Go-module is intended as a template module for Fybrik written in [Go](https://go.dev/).

This module features a read capability of data assets, using a generic server implementation written with [Gin Web Framework](https://pkg.go.dev/github.com/gin-gonic/gin) for Go.

# How a Fybrik Application can access a dataset, using a Go module for Fybrik
To see the Go module for Fybrik in action, you need to take these steps:
1. Install Fybrik
2. Register the Go-module to Fybrik
3. Prepare a data asset and register it in a data catalog
4. Deploy a Fybrik application
5. Access the data asset using the Go-module 

### Install fybrik
Install Fybrik v1.3 using the [Quick Start](https://fybrik.io/v1.3/get-started/quickstart/), without the section of `Install modules`, and make sure to install Fybrik with Katalog as the data catalog.

### Register the Go-module to Fybrik

To register The Go-module as a Fybrik module apply `module.yaml` to the fybrik-system namespace of your cluster.

To install the module:

```bash
kubectl apply -f https://raw.githubusercontent.com/aradhalevy/go-module/go-module-setup/module.yaml -n fybrik-system
```

### Prepare a data asset and register it in a data catalog

First, you should create a new Kubernetes namespace and set it as the active namespace: 

```bash
kubectl create namespace fybrik-notebook-sample
kubectl config set-context --current --namespace=fybrik-notebook-sample
```

This example uses a sample of 100 lines of the [Synthetic Financial Datasets For Fraud Detection](https://www.kaggle.com/ealaxi/paysim1) dataset. Download [`PS_20174392719_1491204439457_log.csv`](https://raw.githubusercontent.com/fybrik/fybrik/master/samples/notebook/PS_20174392719_1491204439457_log.csv) from GitHub. 

Upload the CSV file to an object storage of your choice such as AWS S3. For experimentation you can install localstack to your cluster instead of using a cloud service:

1. Define variables for access key and secret key
      ```bash
      export ACCESS_KEY="myaccesskey"
      export SECRET_KEY="mysecretkey"
      ```
2. Install localstack to the currently active namespace and wait for it to be ready:
      ```bash
      helm repo add localstack-charts https://localstack.github.io/helm-charts
      helm install localstack localstack-charts/localstack \
       --version 0.4.3 \
       --set image.tag="1.2.0" \
       --set startServices="s3" \
       --set service.type=ClusterIP \
       --set livenessProbe.initialDelaySeconds=25
      kubectl wait --for=condition=ready --all pod -n fybrik-notebook-sample --timeout=120s
      ```

3. Create a port-forward to communicate with localstack server:
      ```bash
      kubectl port-forward svc/localstack 4566:4566 &
      ```
4. Use [AWS CLI](https://aws.amazon.com/cli/) to upload the dataset to a new created bucket in the localstack server (make sure to replace /path/to/PS... with the directory you downloaded the data set to):
      ```bash
      export ENDPOINT="http://127.0.0.1:4566"
      export BUCKET="demo"
      export OBJECT_KEY="PS_20174392719_1491204439457_log.csv"
      export FILEPATH="/path/to/PS_20174392719_1491204439457_log.csv"
      export REGION=theshire
      aws configure set aws_access_key_id ${ACCESS_KEY} && aws configure set aws_secret_access_key ${SECRET_KEY}
      aws configure set region ${REGION}
      aws --endpoint-url=${ENDPOINT} s3api create-bucket --bucket ${BUCKET} --region ${REGION} --create-bucket-configuration LocationConstraint=${REGION}
      aws --endpoint-url=${ENDPOINT} s3api put-object --bucket ${BUCKET} --key ${OBJECT_KEY} --body ${FILEPATH}
      ```

In this step you are performing the role of the data owner, registering his data in the data catalog and registering the credentials for accessing the data in the credential manager.

We now explain how to register a dataset in the Katalog data catalog.

Begin by registering the credentials required for accessing the dataset as a kubernetes secret. Replace the values for `access_key` and `secret_key` with the values from the object storage service that you used and run:

```bash
cat << EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
    name: paysim-csv
type: Opaque
stringData:
    access_key: "${ACCESS_KEY}"
    secret_key: "${SECRET_KEY}"
EOF
```

Next, register the data asset itself in the data catalog.
We use port-forwarding to send asset creation requests to the Katalog connector.

```bash
cat << EOF | kubectl apply -f -
apiVersion: katalog.fybrik.io/v1alpha1
kind: Asset
metadata:
  name: paysim-csv
spec:
  secretRef: 
    name: paysim-csv
  details:
    dataFormat: csv
    connection:
      name: s3
      s3:
        endpoint: "http://localstack.fybrik-notebook-sample.svc.cluster.local:4566"
        bucket: "demo"
        object_key: "PS_20174392719_1491204439457_log.csv"
  metadata:
    name: Synthetic Financial Datasets For Fraud Detection
    geography: theshire 
    tags:
      finance: true
EOF
```

### Deploy a Fybrik application

Create a `FybrikApplication` resource to register the notebook workload to the control plane of Fybrik. The value you place in the `dataSetID` field is your asset ID, as explained above. you can run the following to Create a `FybrikApplication` resource for this example:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: app.fybrik.io/v1beta1
kind: FybrikApplication
metadata:
  name: my-notebook
  labels:
    app: my-notebook
spec:
  selector:
    workloadSelector:
      matchLabels:
        app: my-notebook
  appInfo:
    intent: Fraud Detection
  data:
    - dataSetID: "fybrik-notebook-sample/paysim-csv"
      flow: read
      requirements:
        interface: 
          protocol: fybrik-go
EOF
```

Run the following command to wait until the `FybrikApplication` is ready:

```bash
while [[ $(kubectl get fybrikapplication my-notebook -o 'jsonpath={.status.ready}') != "true" ]]; do echo "waiting for FybrikApplication" && sleep 5; done
```

### Access the data asset using the Go-module

In your terminal, run the following command to print the endpoint to use for reading the data. It fetches the code from the `FybrikApplication` resource:

```bash
export ENDPOINT_SCHEME=$(kubectl get fybrikapplication my-notebook -o "jsonpath={.status.assetStates.fybrik-notebook-sample/paysim-csv.endpoint.fybrik-go.scheme}")
export ENDPOINT_HOSTNAME=$(kubectl get fybrikapplication my-notebook -o "jsonpath={.status.assetStates.fybrik-notebook-sample/paysim-csv.endpoint.fybrik-go.hostname}")
export ENDPOINT_PORT=$(kubectl get fybrikapplication my-notebook -o "jsonpath={.status.assetStates.fybrik-notebook-sample/paysim-csv.endpoint.fybrik-go.port}")
export ASSET_NAME="fybrik-notebook-sample%%2Fpaysim-csv"
printf "\n${ENDPOINT_SCHEME}://${ENDPOINT_HOSTNAME}:${ENDPOINT_PORT}/${ASSET_NAME}\n\n"
```

The next steps use the endpoint to read the data in a Kubernetes pod. 

to first set up a basic pod run:

```bash
kubectl run mypod --image=docker.io/library/alpine:3.18 -i --tty -- sh
```

Now, in the shell of the pod that opened up, run the following (make sure to put in the ENDPOINT you printed):

```bash
apk --no-cache add curl util-linux && curl -L -o /tmp/data.csv ('ENTER ENDPOINT HERE') && column -s, -t < /tmp/data.csv
```

And you should be able to see the  data set.

### Cleanup 

When you're finished experimenting with a sample, you may clean up as follows:

1. Stop ```kubectl port-forward``` processes (e.g., using ```pkill kubectl```)
2. Delete the namespace created for this sample:

```bash
kubectl delete namespace fybrik-notebook-sample
```
