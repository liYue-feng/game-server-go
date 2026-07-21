# Payment Fail-Closed Release Design

## Context

The current payment code is not a partial production integration. It accepts an unsigned JSON body, exposes an HTTP callback without WeChat Pay V3 headers, treats every parsed notification as successful, logs instead of granting an entitlement, and records `Delivered` after the empty grant step. A forged callback can therefore produce a false success push, while a status-write retry can repeat a future non-idempotent grant.

The protobuf contract also cannot start a real WeChat payment: `CreateOrderResp` contains only `order_no`, with no prepay identifier or client-side signing fields. Products 3 and 4 describe monthly-card and hero entitlements that do not exist in `PlayerArchive`. Merchant credentials and the API V3 decryption key are intentionally absent from source control.

## Decision

This release disables production payment fail-closed. It preserves the protobuf IDs and Unity `PaymentSessionService`, but the server returns a correlated business error for `CreateOrderReq`, never starts the `:8081` callback listener, never accepts callback data, never creates an order, never mutates an archive, and never emits `PayResultNotify`.

`wechat.payment_enabled` defaults to `false`. Setting it to `true` is a startup configuration error with an explicit message that the secure provider is unavailable. A future payment release must replace that startup guard only after adding the complete external and business contract.

## Server Boundaries

- `internal/payment.Handler` is a disabled protocol boundary with no database, Redis, verifier, or pusher dependency.
- `Handler.CreateOrder` always returns a protobuf business error and performs no side effect.
- The unsigned callback handler, body-only verifier, placeholder product map, logging-only delivery, order-store adapter, and payment pusher are removed.
- `cmd/server` registers the disabled `CreateOrderReq` route in both development and production runtimes so clients receive a correlated error rather than a silent unsupported route.
- `cmd/server` does not construct an HTTP payment server or listen on port `8081`.
- `newRuntime` rejects `payment_enabled: true` before opening MySQL or Redis.

## Future Enablement Contract

Payment may be enabled only when one coherent implementation provides all of the following:

1. WeChat Pay V3 request creation and the prepay/signing fields required by the Unity client.
2. Platform-certificate verification using `Wechatpay-Serial`, `Wechatpay-Timestamp`, `Wechatpay-Nonce`, and `Wechatpay-Signature` over the exact raw body.
3. Timestamp/replay policy and API V3 AES-GCM resource decryption.
4. Merchant ID, App ID, transaction state, currency, order number, and exact amount validation.
5. Explicit entitlement fields for every offered product.
6. One MySQL transaction that locks the order, applies the entitlement, records the platform transaction ID, and marks delivery exactly once.
7. Retry, duplicate callback, offline push, and recovery tests.

No development shortcut may share the production callback route or production enable flag.

## Documentation

Authoritative repository instructions and transport comments must describe the 10-byte little-endian frame `[Length uint32][MsgID uint16][Seq uint32]`, ordinary nonzero request sequences, echoed response sequences, and `seq=0` pushes. Removed 6-byte descriptions are release blockers.

## Acceptance

- A `CreateOrderReq` receives a matching nonzero-sequence `ErrorResp` and creates no order.
- Payment-disabled startup opens the normal WebSocket server but no callback listener.
- `payment_enabled: true` fails before any external store is opened.
- No production code accepts a body-only payment callback or reports a delivered entitlement.
- Go test/vet/build and protocol/integration verification remain green.
- Current-state docs contain no 6-byte frame description.

