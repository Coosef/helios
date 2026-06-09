# Helios - Claude Development Rules

## Project Overview

Helios is a SaaS-based hybrid data protection platform.

The product provides:

* SaaS management panel
* Windows/Linux backup agents
* Customer-owned storage support
* Helios Cloud storage support
* Secure backup and restore
* Device licensing
* Storage quota licensing

## Development Rules

Always work sprint by sprint.

Never implement features from future sprints unless explicitly requested.

Security has higher priority than speed.

Restore reliability has higher priority than backup speed.

Never hardcode secrets.

Never store encryption keys in source code.

Always use environment variables for sensitive configuration.

Always update documentation after completing a sprint.

Always create tests for new functionality.

Always use English variable names.

Always use English comments.

## Technical Decisions

Agent Language:
Go

Backend:
FastAPI

Database:
PostgreSQL

Queue:
Redis

Frontend:
React / Next.js

Installer:
Inno Setup

Compression:
ZSTD

Encryption:
AES-256-GCM

Hashing:
BLAKE3 preferred, SHA256 acceptable

## Architecture Principles

* Multi-tenant first
* Security first
* Cloud-native
* API-first
* Backup integrity first
* Restore correctness first

## Coding Standards

* Clean Architecture
* SOLID principles
* Structured logging
* Dependency Injection where appropriate
* Unit tests
* Integration tests

## Forbidden

Do not use mock data unless requested.

Do not skip error handling.

Do not create placeholder security implementations in production code.

Do not simplify encryption logic.

Do not remove update signature validation.
