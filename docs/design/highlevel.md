# Highlevel Design Overview

## Introduction

Azure Resource Notifications (ARN) is a service for sending notifications about changes to Azure resources.
This allows subscribing services to be notified when a resource is created, updated, or deleted. It also provides
for snapshoting to make sure the data is in sync with the service resources at various intervals.

This document provides a high-level overview of the ARN client for Go. It is intended to provide a general
understanding of the client and its capabilities.

## Design Goals

The ARN client for Go is designed to provide a simple and easy-to-use interface for subscribing to Azure Resource
Notifications. The client is designed to be flexible and extensible, allowing for transitioning to new model versions
without breaking the client. The client is also designed to be efficient and performant, with a focus on reducing
garbage collection (GC) cycles.

## ARN Data Flow

The ARN client has two main ways of sending data:
- Extremely small data(< 4k): Send a single message to the ARN service endpoint containing the data.
- Other: Upload message to blob storage, signal the ARN service endpoint to read the blob and process the data.

The ARN message is designed to allow sending multiple events in a single message. For most cases, this means
you will always be sending the data to blob storage and signaling the ARN service endpoint to read the blob.

In the future, there may be an allowable total size of 1MiB for the entire message. This SDK is set to use the current
4KiB limit.

## Structure

```bash
.
├── client
├── docs
│   └── design
│       ├── highlevel.md
│       └── img
├── internal
│   ├── build
│   └── conn
│       ├── http
│       ├── maxvals
│       └── storage
└── models
    ├── README.md
    ├── internal
    │   └── private
    ├── v3
    │   ├── msgs
    │   └── schema
    │       ├── envelope
    │       └── types
    └── version
```

The ARN client for Go is organized into the following directories:
- client: Contains the client package, which provides the main functionality for sending to the ARN service. This is agnostic to the model type.
- docs: Contains documentation for the ARN client for Go that is not appropriate for the godoc or README.
- internal/: Contains internal packages that are not intended for public use.
  - internal/build: Contains build information that can used by the linker.
  - internal/conn: Contains connection abstraction information for the ARN client.
  - internal/conn/http: Contains the HTTP connection implementation for the ARN client so we can talk to the ARN service HTTP endpoints.
  - internal/conn/maxvals: Contains various maximum values for the ARN client.
  - internal/conn/storage: Contains the blob storage connection implementation for the ARN client so we can talk to Azure blob storage.
- models/: Contains definitions for interface and error types that all models must implement.
  - models/v3: Contains the v3 model definitions for the ARN client.
    - models/v3/msgs: Contains the v3 implementation of the `models.Notifications` interface.
    - models/v3/schema: Contains directories holding various v3 schema types
      - models/v3/schema/envelope: Contains the Event type definition, which is based around the Event Grid format that ARN used to use. This wraps the actual resource data.
      - models/v3/schema/types: Contains all the type definitions used in an ARN v3 model message.

## Adding support for a new model

As of now, the current model is version 3, with a draft for a version 5. To add support for a new model, you need
to implement the types in the `models` package. Once it is in place, a user can simply switch to the new model type.
