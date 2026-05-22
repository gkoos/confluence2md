### Implementation

- Use `fileId` as idempotency key.
- Catch `E11000` and return 200.

1. Create index
1. Deploy migration