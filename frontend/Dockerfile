FROM node:24.22.1-alpine@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53 AS build

WORKDIR /app

ARG VITE_API_URL
ARG VITE_MCP_API_URL
ENV VITE_API_URL=$VITE_API_URL
ENV VITE_MCP_API_URL=$VITE_MCP_API_URL

COPY package.json package-lock.json* ./
RUN npm ci

COPY index.html vite.config.js ./
COPY scripts/production-source-boundary.mjs ./scripts/production-source-boundary.mjs
COPY scripts/monaco-sanitizer.mjs ./scripts/monaco-sanitizer.mjs
COPY scripts/initial-js-budget.mjs ./scripts/initial-js-budget.mjs
COPY public ./public
COPY src ./src

RUN npm run build

FROM nginxinc/nginx-unprivileged:1.31.7-alpine@sha256:b5047b9b76845eac124358373c6852377c52e4b05834a47647e955dd91cb8f1e

USER root
RUN apk upgrade --no-cache libcrypto3 libssl3 libexpat libuuid \
    && apk add --upgrade --no-cache 'pcre2>=10.49-r0' 'tiff>=4.7.2-r0' 'libx11>=1.8.14-r0' 'nginx>=1.30.6-r0' 'brotli-libs>=1.2.1-r0'
USER 101

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 3210
