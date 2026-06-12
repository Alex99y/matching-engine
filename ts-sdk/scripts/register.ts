/// <reference types="node" />
import { MatchingEngineClient } from "../build/index.js";

const client = new MatchingEngineClient("localhost", 4000, { allowInsecure: true });

try {
  await client.register({
    username: "alex",
    email: "alex@me.com",
    password: "pass123456",
  });
  console.log("User 'alex' registered successfully.");
} catch (err) {
  console.error("Registration failed:", err instanceof Error ? err.message : err);
  process.exit(1);
}


try {
  await client.register({
    username:"bob",
    email:"bob@example.com",
    password:"bobpass123456"
  });
  console.log("User 'bob' registered successfully.");
} catch (err) {
  console.error("Registration failed:", err instanceof Error ? err.message : err);
  process.exit(1);
}