FROM node:20-alpine AS builder
WORKDIR /app
COPY frontend/package*.json ./frontend/
COPY pkg/instrumentation-js ./pkg/instrumentation-js/

# Build otel-rxjs-ws so dist/ exists before Vite resolves @marz32one/otel-rxjs-ws.
WORKDIR /app/pkg/instrumentation-js/packages/otel-rxjs-ws
RUN npm install --ignore-scripts
RUN npm run build

WORKDIR /app/frontend
RUN npm install
COPY frontend/ ./
ARG VITE_API_URL=http://localhost:8088
ARG VITE_API_V1_URL=http://localhost:8089
ARG VITE_WS_URL=ws://localhost:8082
ARG VITE_OTEL_COLLECTOR_URL=http://localhost:4318
ENV VITE_API_URL=$VITE_API_URL
ENV VITE_API_V1_URL=$VITE_API_V1_URL
ENV VITE_WS_URL=$VITE_WS_URL
ENV VITE_OTEL_COLLECTOR_URL=$VITE_OTEL_COLLECTOR_URL
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/frontend/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 3000
