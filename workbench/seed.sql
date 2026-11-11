-- Acceptance-test identity for the local workbench: an admin user with a personal API key,
-- mirroring the seed user's key that the infrastructure module authenticates with.
-- Console stores keys as salt.sha512(secret + salt + GLOBAL_HASH_SECRET); the workbench
-- leaves GLOBAL_HASH_SECRET unset, so the seed below is Console's built-in default.
INSERT INTO newjitsu."UserProfile" (id, name, email, admin, "loginProvider", "externalId")
VALUES ('tfacc', 'tfacc', 'tfacc@localhost', true, 'tfacc', 'tfacc')
ON CONFLICT (id) DO NOTHING;

INSERT INTO newjitsu."UserApiToken" (id, hint, hash, "userId")
VALUES (
  'tfacc',
  'wor*key',
  'tfacc.' || encode(sha512(convert_to('workbench-only-key' || 'tfacc' || 'dea42a58-acf4-45af-85bb-e77e94bd5025', 'UTF8')), 'hex'),
  'tfacc'
)
ON CONFLICT (id) DO NOTHING;
