FROM node:22-alpine

WORKDIR /app
COPY package*.json ./
RUN npm install \
    && npm install --no-save \
        @rolldown/binding-linux-x64-musl@1.2.11 \
        lightningcss-linux-x64-musl@1.33.0
COPY . .

EXPOSE 5173
CMD ["npm", "run", "dev"]
