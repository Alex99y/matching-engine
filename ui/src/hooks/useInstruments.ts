import { useEffect, useState } from "react";
import type { Instrument, MatchingEngineClient } from "ts-sdk";

// Fetches the instrument list once per client (for decimals lookups) and
// keeps it around for the lifetime of the component tree that needs it.
// Public endpoint — works for guests too.
export function useInstruments(client: MatchingEngineClient | null): Instrument[] {
  const [instruments, setInstruments] = useState<Instrument[]>([]);

  useEffect(() => {
    if (!client) {
      setInstruments([]);
      return;
    }
    let active = true;
    client.getInstruments().then((list) => {
      if (active) setInstruments(list);
    }).catch(() => {});
    return () => { active = false; };
  }, [client]);

  return instruments;
}
