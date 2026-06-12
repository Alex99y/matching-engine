## TODO List
- Store sessions in the DB and implement the logout feature
- Move from encoded/json to protobuf for event messages
- Add a dead letter queue (DLX) for orders that cannot be processed
- Make sure that we are not exposing server errors in `api`
- Allow multisession and API keys
- Implement bots that copy orderbooks from other exchanges