# Installation

This file contains the commands to run this local implementation in your Kubernetes cluster.

## Prerequisites

Use a Kubernetes 1.33 or newer cluster. For clusters from 1.27 through 1.32, enable the `InPlacePodVerticalScaling` feature gate.

Make sure these tools are available in your shell:

```powershell
kubectl version --client
helm version
docker version
```

Confirm that `kubectl` points at the cluster you want to use:

```powershell
kubectl config current-context
kubectl get nodes
```

## Install The Local Implementation

The published Helm chart uses the published Google image. To run the changes in this checkout, build and push your own image, then install the local chart from `.\charts\kube-startup-cpu-boost`.

Set image values:

```powershell
cd D:\code\my-repos\kube-startup-cpu-boost

$ImageRepository = "<your-registry>/kube-startup-cpu-boost"
$ImageTag = "dashboard-events"
```

Build and push the controller image:

```powershell
docker build -t "${ImageRepository}:${ImageTag}" .
docker push "${ImageRepository}:${ImageTag}"
```

Install or upgrade the controller without a dashboard endpoint:

```powershell
helm upgrade --install kube-startup-cpu-boost .\charts\kube-startup-cpu-boost `
  -n kube-startup-cpu-boost-system `
  --create-namespace `
  --set controllerManager.manager.image.repository="$ImageRepository" `
  --set controllerManager.manager.image.tag="$ImageTag"
```

Install or upgrade the controller with a dashboard event endpoint:

```powershell
$DashboardEventUrl = "http://your-dashboard-service.your-namespace.svc.cluster.local/events"

helm upgrade --install kube-startup-cpu-boost .\charts\kube-startup-cpu-boost `
  -n kube-startup-cpu-boost-system `
  --create-namespace `
  --set controllerManager.manager.image.repository="$ImageRepository" `
  --set controllerManager.manager.image.tag="$ImageTag" `
  --set controllerManager.manager.env.dashboardEventUrl="$DashboardEventUrl"
```

## Verify The Controller

Check the installed resources:

```powershell
kubectl get pods -n kube-startup-cpu-boost-system
kubectl get deployment kube-startup-cpu-boost-controller-manager -n kube-startup-cpu-boost-system
```

Follow the controller logs:

```powershell
kubectl logs -n kube-startup-cpu-boost-system `
  deploy/kube-startup-cpu-boost-controller-manager `
  -c manager `
  -f
```

Expected log messages include:

```text
container CPU resources increased
pod CPU resources increased
startup_cpu_boost_event scope="container" eventType="increase"
startup_cpu_boost_event scope="pod" eventType="increase"
startup_cpu_boost_event scope="container" eventType="decrease"
startup_cpu_boost_event scope="pod" eventType="decrease"
```

## Create A Test Boost

Create a namespace:

```powershell
kubectl create namespace demo
```

Create a `StartupCPUBoost` configuration:

```powershell
@"
apiVersion: autoscaling.x-k8s.io/v1alpha1
kind: StartupCPUBoost
metadata:
  name: demo-boost
  namespace: demo
selector:
  matchLabels:
    app: cpu-boost-demo
spec:
  resourcePolicy:
    containerPolicies:
    - containerName: app
      percentageIncrease:
        value: 100
  durationPolicy:
    fixedDuration:
      unit: Seconds
      value: 60
"@ | kubectl apply -f -
```

Create a matching workload:

```powershell
@"
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cpu-boost-demo
  namespace: demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cpu-boost-demo
  template:
    metadata:
      labels:
        app: cpu-boost-demo
    spec:
      containers:
      - name: app
        image: nginx:1.27
        resources:
          requests:
            cpu: 100m
            memory: 64Mi
          limits:
            cpu: 200m
            memory: 128Mi
"@ | kubectl apply -f -
```

Inspect the pod resources and annotations:

```powershell
$PodName = kubectl get pod -n demo -l app=cpu-boost-demo -o jsonpath="{.items[0].metadata.name}"

kubectl get pod $PodName -n demo `
  -o jsonpath="{.metadata.annotations.autoscaling\.x-k8s\.io/startup-cpu-boost}{'\n'}"

kubectl get pod $PodName -n demo `
  -o jsonpath="{.spec.containers[0].resources}{'\n'}"
```

After the fixed duration expires, check that the CPU values are restored and that the controller emitted `decrease` events.

## Uninstall

Remove the controller:

```powershell
helm uninstall kube-startup-cpu-boost -n kube-startup-cpu-boost-system
kubectl delete namespace kube-startup-cpu-boost-system
```

Remove the demo workload:

```powershell
kubectl delete namespace demo
```
