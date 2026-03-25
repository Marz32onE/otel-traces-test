FROM node:20-alpine AS builder
WORKDIR /app

COPY frontend-ws-trace/package*.json ./frontend-ws-trace/
COPY pkg/instrumentation-js ./pkg/instrumentation-js/

WORKDIR /app/frontend-ws-trace
RUN npm install

COPY frontend-ws-trace/ ./

ARG VITE_WS_URL=ws://localhost:8085/ws
ARG VITE_OTEL_COLLECTOR_URL=http://localhost:4318
ENV VITE_WS_URL=$VITE_WS_URL
ENV VITE_OTEL_COLLECTOR_URL=$VITE_OTEL_COLLECTOR_URL

RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/frontend-ws-trace/dist /usr/share/nginx/html
COPY frontend-ws-trace/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 3000

