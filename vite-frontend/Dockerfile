# 多阶段构建 - 构建阶段（在构建机原生平台编译静态资源；使用已修复 CVE-2026-111497 的 Node.js）
FROM --platform=$BUILDPLATFORM node:22.23.4-alpine AS builder

WORKDIR /app

COPY package*.json ./
RUN npm install --legacy-peer-deps

COPY . .
RUN npm run build

# 生产阶段
FROM nginx:stable-alpine AS production-stage

COPY nginx.conf /etc/nginx/nginx.conf

COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"] 