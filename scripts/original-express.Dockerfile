FROM public.ecr.aws/docker/library/node:20-alpine
WORKDIR /app
COPY package.json ./
RUN npm install --omit=dev --ignore-scripts --no-audit --no-fund
COPY server.js ./
COPY public ./public
ENV NODE_ENV=production
CMD ["npm", "start"]
