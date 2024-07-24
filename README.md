# ARN Client for Go

![Logo](logo.webp "Azure Resource Notifications")

This package provides a Go client for Azure Resource Notifications.

**Important Note:** This package is intended for use by Microsoft employees only. If you are a Microsoft employee, please contact the ARN team for assistance in getting started.

All basic usage information for the client is available in the `client` package godoc. To effectively use this client, you will need to contact the ARN team to obtain the necessary information for configuring your service to utilize the client.

## Notes:

This package is based on a previous package developed by another developer, which was not in active use. After utilizing that package for some time, we identified various improvements that could be made. This is a complete re-write to incorporate those improvements. The original package was crucial in bootstrapping this project and shares some code with this new version.

Key changes and improvements in this package include:

- **Removal of auto-generated code:** This reduces unnecessary pointer usage, which improves garbage collection (GC) efficiency. Reducing GC cycles is critical for performance in large Go services.
- **Independence from any specific Model version:** The new design allows for the creation of new models that satisfy our Notification type, enabling future updates without breaking the client. While we use interfaces for Notifications and Event types to allow for future model abstraction, the actual model consists of concrete types defined for the user.
- **Support for synchronous and asynchronous methods:** This provides flexibility in how the client can be used.
- **Enhanced documentation:** More comprehensive documentation makes it easier to understand and use the client.
- **Improved testing:** Tests have been revamped to be faster and cleaner, replacing ginkgo tests with standard Go tests and eliminating the use of mock libraries.

This re-write significantly benefited from the groundwork laid by the original package, making the process of deciphering the ARN API much easier.
