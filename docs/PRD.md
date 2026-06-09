You are a senior software architect and full-stack developer.

I want to build a SaaS-based hybrid data protection platform named “Helios”, the **Helios Data Protection Platform**, a product of **Beyz System A.Ş.**

The product will work like a lightweight alternative to Veeam/Acronis for SMBs, hotels, MSPs and multi-location companies.

The system will have:

1. A cloud SaaS management panel
2. A Windows/Linux backup agent
3. Customer-owned storage support
4. Helios Cloud storage support
5. Device-based licensing
6. Storage quota-based licensing
7. Secure client-side encryption
8. Agent auto-update system

Important technical decisions:

* Agent language: Go
* Windows agent must run as Windows Service
* Linux agent should support systemd later
* Installer: Inno Setup
* Agent files:

  * beyz-backup-agent.exe
  * beyz-backup-updater.exe
* Config path:

  * C:\ProgramData\BeyzBackup\config.yaml
* Log path:

  * C:\ProgramData\BeyzBackup\logs\
* Install path:

  * C:\Program Files\BeyzBackup\
* Encryption:

  * AES-256-GCM
* Compression:

  * ZSTD
* Hashing:

  * SHA256 or BLAKE3
* Backup model:

  * Full backup
  * Incremental backup later
  * Chunk-based backup
  * Restore point manifest
  * Reference counting later
* Transfer:

  * Resumable upload
  * Chunk upload
  * S3/MinIO support first
* Security:

  * Client-side encryption
  * Signed update manifest
  * SHA256 validation
  * Agent fingerprint
  * Enrollment token
  * Heartbeat
  * Task polling
  * Audit log
  * Tamper detection

Do not build the whole backup product in one step.

Start with Sprint 1 only.

Sprint 1 goal:
Create the foundation of the backup agent and installer.

Sprint 1 requirements:

1. Create a production-ready Go project structure.
2. Create beyz-backup-agent.exe.
3. The agent must be able to run as a Windows Service.
4. Add basic Linux systemd preparation, but Windows is the priority.
5. Add config management using config.yaml.
6. Add structured logging.
7. Add enrollment token support.
8. Add SaaS API registration placeholder.
9. Add heartbeat sender.
10. Add task polling placeholder.
11. Create beyz-backup-updater.exe.
12. Updater should have placeholder logic for:

    * update manifest check
    * signature verification
    * SHA256 validation
    * stopping agent service
    * replacing binary
    * restarting service
    * rollback
13. Create an Inno Setup installer script.
14. Installer should install both agent and updater.
15. Installer should create required folders.
16. Installer should register Windows Service.
17. Installer should start the service after installation.
18. Installer should support enrollment token input or command-line parameter.
19. Provide all source code.
20. Provide build commands.
21. Provide folder structure.
22. Provide test instructions.
23. Provide security notes.
24. Do not use mock explanations only; generate actual working starter code.

Expected output:

* Complete folder structure
* Go source files
* Config sample
* Inno Setup script
* Build commands
* Windows service install/start instructions
* Explanation of how to test heartbeat and enrollment
* Clear next steps for Sprint 2

Important:
Use clean, maintainable and production-oriented code.
Do not hardcode secrets.
Do not put encryption keys in source code.
Use English variable names and code comments.
