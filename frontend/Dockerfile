FROM node:24-alpine@sha256:2bdb65ed1dab192432bc31c95f94155ca5ad7fc1392fb7eb7526ab682fa5bf14 AS build

WORKDIR /app

ARG VITE_API_URL
ARG VITE_MCP_API_URL
ENV VITE_API_URL=$VITE_API_URL
ENV VITE_MCP_API_URL=$VITE_MCP_API_URL

COPY package.json package-lock.json* ./
RUN npm ci

COPY index.html vite.config.js ./
COPY public ./public
COPY src ./src

RUN npm run build

FROM nginxinc/nginx-unprivileged:1.31-alpine@sha256:02c985e54bee40c14c6de677226f394d96c661944185d407e77ca73602f15e1b

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 3210
