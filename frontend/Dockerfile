FROM node:24.21.0-alpine@sha256:be80f76cf40ec8e42b9bec49f60a55e0660f30af58d3e5a25530785b30ea67e2 AS build

WORKDIR /app

ARG VITE_API_URL
ARG VITE_MCP_API_URL
ENV VITE_API_URL=$VITE_API_URL
ENV VITE_MCP_API_URL=$VITE_MCP_API_URL

COPY package.json package-lock.json* ./
RUN npm ci

COPY index.html vite.config.js ./
COPY scripts/production-source-boundary.mjs ./scripts/production-source-boundary.mjs
COPY public ./public
COPY src ./src

RUN npm run build

FROM nginxinc/nginx-unprivileged:1.30-alpine@sha256:44e36330f74d4f3a1d4e222acca9e23b401fb87811a7597024502bb759c4dd49

USER root
RUN apk upgrade --no-cache libcrypto3 libssl3 libexpat libuuid
USER 101

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 3210
