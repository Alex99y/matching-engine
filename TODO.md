## TODO List
- Store sessions in the DB and implement the logout feature
- Move from encoded/json to protobuf for event messages
- Add a dead letter queue (DLX) with a ttl, in case that the DB goes down we wont spam events to the queue.