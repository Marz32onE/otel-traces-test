FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
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
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 3000
