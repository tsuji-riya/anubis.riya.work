FROM nginxinc/nginx-unprivileged:1.31.7-alpine
COPY nginx-no-store.conf /etc/nginx/conf.d/no-store.conf
COPY site/ /usr/share/nginx/html/
