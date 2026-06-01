FROM node:26-alpine@sha256:7c6af15abe4e3de859690e7db171d0d711bf37d27528eddfe625b2fe89e097f8 AS build

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

FROM nginxinc/nginx-unprivileged:1.27-alpine@sha256:65e3e85dbaed8ba248841d9d58a899b6197106c23cb0ff1a132b7bfe0547e4c0

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 3210
