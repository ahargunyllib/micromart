FROM migrate/migrate:latest

# Copy migration files into the image
COPY *.sql /migrations/

# Set the working directory
WORKDIR /migrations

# The entrypoint is already set by the base image
# Usage: docker run <image> -database <url> up
