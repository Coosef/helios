# Architecture

## High Level Components

1. SaaS Platform
2. Backup Agent
3. Update Service
4. Storage Layer
5. Restore Engine

---

## SaaS Platform

Responsibilities:

* Tenant Management
* User Management
* RBAC
* Licensing
* Backup Policies
* Monitoring
* Reporting

---

## Agent

Responsibilities:

* Enrollment
* Heartbeat
* Task Polling
* Backup Execution
* Restore Execution
* Compression
* Encryption
* Upload

---

## Update Service

Responsibilities:

* Version Check
* Manifest Validation
* Signature Validation
* Binary Download
* Rollback

---

## Storage Layer

Supported Targets:

* SMB
* SFTP
* S3
* MinIO
* Beyz Cloud Storage

---

## Data Flow

Files
↓
Chunking
↓
Hashing
↓
Compression (ZSTD)
↓
Encryption (AES-256-GCM)
↓
Upload
↓
Manifest Creation
↓
Restore Point
