# Package conn

`conn` and its subpackages `conn/http` and `conn/storage` provide all calls needed for the ARN service.

This package is a little non-standard. Normally when I make a `conn` package it encapsulates all the calls to a service.

In this case, `conn` uses the models.Notification.SendEvent() method to send
events to the ARN service. This allows us to let the specific model's notifications package handle changes to the event data as required if the data size gets too large. This is a consequence of ARN being backed by Kusto which cannot handle large batches of data without going to blob storage.

`conn/http` is a wrapper around azcore.Client which is a wrapper around http.Client. This simply encapsulates the client endpoing and request specifics for the ARN service.

`conn/storage` is a wrapper around azblob. This encapsulates the specifics of the ARN blob storage container.
