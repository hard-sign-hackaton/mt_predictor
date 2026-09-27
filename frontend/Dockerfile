FROM node:22-alpine

WORKDIR /app
COPY package*.json ./
RUN set -eu; \
    npm install --include=optional; \
    for package in 'rolldown:@rolldown/binding-linux-x64-musl' 'lightningcss:lightningcss-linux-x64-musl'; do \
        parent="${package%%:*}"; \
        binding="${package#*:}"; \
        version="$(node -p "require('./node_modules/${parent}/package.json').optionalDependencies['${binding}']")"; \
        tarball="$(npm pack "${binding}@${version}" --pack-destination /tmp --silent)"; \
        mkdir -p "node_modules/${binding}"; \
        tar -xzf "/tmp/${tarball}" -C "node_modules/${binding}" --strip-components=1; \
        rm "/tmp/${tarball}"; \
    done
COPY . .

EXPOSE 5173
CMD ["npm", "run", "dev"]
