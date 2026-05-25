# Puppet Server Certificate Generation via HTTP API

### Step 1: Generate a Private Key

```sh
openssl genrsa -out <certname>.key 4096
```

### Step 2: Create a CSR

The CN must match the certname exactly.

```sh
openssl req -new -key <certname>.key -out <certname>.csr \
  -subj "/CN=<certname>"
```

### Step 3: Submit the CSR to Puppet CA

```sh
curl -sk -X PUT \
  -H "Content-Type: text/plain" \
  --data-binary @<certname>.csr \
  https://puppetserver-enableit-puppet:8140/puppet-ca/v1/certificate_request/<certname>
```

The certificate will be auto-signed by the Puppet CA.

### Step 4: Download the Signed Certificate

```sh
curl -sk https://puppetserver-enableit-puppet:8140/puppet-ca/v1/certificate/<certname> > <certname>.crt
```

### Step 5: Download the CA Certificate

```sh
curl -sk https://puppetserver-enableit-puppet:8140/puppet-ca/v1/certificate/ca > ca.crt
```

---

## Output Files

| File              | Description                     |
|-------------------|---------------------------------|
| `<certname>.key`  | Private key (generated locally) |
| `<certname>.crt`  | Signed certificate              |
| `ca.crt`          | CA certificate (trust anchor)   |