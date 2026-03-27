FROM node:14-alpine
WORKDIR /app

COPY pkg/instrumentation-js ./pkg/instrumentation-js/
COPY ws-node-backend/package*.json ./ws-node-backend/

# Build otel-ws so dist/ exists before ws-node-backend npm install links it
WORKDIR /app/pkg/instrumentation-js/packages/otel-ws
RUN npm install --ignore-scripts
RUN npm run build

WORKDIR /app/ws-node-backend
RUN npm install

COPY ws-node-backend/ .
RUN npm run build

CMD ["node", "dist/index.js"]
