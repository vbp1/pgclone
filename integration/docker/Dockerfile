FROM postgres:15

ARG ROLE=primary
ENV ROLE=$ROLE
ENV POSTGRES_PASSWORD=postgres

RUN apt-get update && apt-get install -y rsync openssh-server && rm -rf /var/lib/apt/lists/*

# add ssh keys later via volume
RUN mkdir -p /var/run/sshd && echo "PermitRootLogin yes" >> /etc/ssh/sshd_config

# Generate key pair for postgres user and add to authorized_keys for passwordless SSH between containers
RUN mkdir -p /var/lib/postgresql/.ssh && \
    ssh-keygen -t rsa -q -N "" -f /var/lib/postgresql/.ssh/id_rsa && \
    cat /var/lib/postgresql/.ssh/id_rsa.pub >> /var/lib/postgresql/.ssh/authorized_keys && \
    chown -R postgres:postgres /var/lib/postgresql/.ssh && \
    chmod 700 /var/lib/postgresql && \
    chmod 700 /var/lib/postgresql/.ssh && \
    chmod 600 /var/lib/postgresql/.ssh/authorized_keys

# PostgreSQL config for primary (replication-friendly)
COPY pg_hba.conf /pg_hba.conf
COPY postgresql.conf /postgresql.conf
# Init script copies configs into PGDATA on first run
COPY init_pgclone.sh /docker-entrypoint-initdb.d/init_pgclone.sh
RUN chmod +x /docker-entrypoint-initdb.d/init_pgclone.sh

COPY pgclone /usr/bin/pgclone
RUN chmod +x /usr/bin/pgclone

ENTRYPOINT ["/bin/bash", "-c", "service ssh start && docker-entrypoint.sh postgres"] 