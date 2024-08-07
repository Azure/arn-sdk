# ARN Client for Go

This package provides a Go client for Azure Resource Notifications.

All basic usage information for the client is available in the `client` package godoc. To effectively use this client, you will need to contact the ARN team to obtain the necessary information for configuring your service to utilize the client.

## Notes:

This package is based on a previous package developed (internally at Microsoft) by another developer, which was not in active use for sending data. After utilizing that package for some time, we identified various improvements that could be made. This is a re-write to incorporate those improvements. The original package was crucial in bootstrapping this project and shares some code with this new version.

Key changes and improvements in this package include:

- **Removal of auto-generated code:** This reduces unnecessary pointer usage, which improves garbage collection (GC) efficiency. Reducing GC cycles is critical for performance in large Go services.
- **Independence from any specific Model version:** The new design allows for the creation of new models that satisfy our Notification type, enabling future updates without breaking the client. While we use interfaces for Notifications and Event types to allow for future model abstraction, the actual model consists of concrete types defined for the user.
- **Support for synchronous and asynchronous methods:** This provides flexibility in how the client can be used.
- **Enhanced documentation:** More comprehensive documentation makes it easier to understand and use the client.
- **Improved testing:** Tests have been revamped to be faster and cleaner, replacing ginkgo tests with standard Go tests and eliminating the use of mock libraries.
- **Fixes constant value address issuess:** The original package defined some constants for enumermated values, but the SDK required the address of these constants. This required redefining the constants as variables to get their address. This fixes that issue.
- **Moved logging to slog.Logger:** Originally this used a third party package for logging. Prefer an slog.Logger to allow logging packages to be swapped out.

Removed features:

- **Deletion of storage containers:** The original package had a feature to delete storage containers.
However this feature would have several clients that share the same storage calling deletes.
This seemed inefficient and potentially dangerous, so it was removed. Instead we opt for the user
to set storage containers to be deleted in the storage account. One reason this was probably done is
that Azure Blob Storage can only delete on a daily period, where we should have at least hourly granularity.

This re-write significantly benefited from the groundwork laid by the original package, making the process of deciphering the ARN API much easier.

# Third Party Libraries

This package uses the following third-party libraries:

* github.com/go-json-experiment/json
* github.com/google/uuid
*	github.com/kylelemons/godebug

These package have sub-dependencies as follows:

* github.com/jedib0t/go-pretty/v6
*	github.com/mattn/go-runewidth
*	github.com/rivo/uniseg
*	github.com/sanity-io/litter
*	golang.org/x/net
*	golang.org/x/sys
*	golang.org/x/text

# Trademarks

This project may contain trademarks or logos for projects, products, or services. Authorized use of Microsoft trademarks or logos is subject to and must follow Microsoft’s Trademark & Brand Guidelines. Use of Microsoft trademarks or logos in modified versions of this project must not cause confusion or imply Microsoft sponsorship. Any use of third-party trademarks or logos are subject to those third-party’s policies.
