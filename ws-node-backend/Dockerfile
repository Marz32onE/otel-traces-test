FROM node:14-alpine AS builder
WORKDIR /app

# Copy whole repo for local file: dependency resolution via the `file:` path.
# (We only build this subproject, but Docker needs the referenced path.)
COPY . .

WORKDIR /app/ws-node-backend
RUN npm install

RUN npm run build

CMD ["npm", "run", "start"]

