# otel-traces-test Helm Chart

與專案 docker-compose 相同的架構：Frontend、API、Worker、NATS、Tempo、Grafana、OTel Collector。

## 共用設定

- **config/tempo.yaml**：Tempo 設定（docker-compose 與 Helm 共用）
- **config/otel-collector.yaml**：OTel Collector 設定（共用；Helm 安裝時會將 `tempo:4317` 替換為 ` release-name-tempo:4317`）

docker-compose 掛載路徑為 `./charts/otel-traces-test/config/`。

## 使用方式（Kind）

假設已建立 Kind cluster 且已安裝 kubectl、helm。預設安裝到 **namespace: otel**（`HELM_NAMESPACE=otel`）。

```bash
# 建置 api/worker/frontend 映像並載入 Kind（必須先做，否則本地映像會 ImagePullBackOff）
make kind-build KIND_CLUSTER=<your-cluster-name>

# 安裝到 namespace otel
make kind-install
# 或一次完成：make kind-up

# 卸載
make kind-down
```

api/worker/frontend 使用 `imagePullPolicy: Never`，只會使用本機 build 並經 `kind load docker-image` 載入的映像；未執行 `kind-build` 會出現 ImagePullBackOff。

## NodePort

所有服務皆為 NodePort，埠位在 `values.yaml` 的 `nodePorts`（範圍 30000–32767）。需從 host 存取時可用 `kubectl port-forward` 或 Ingress。
