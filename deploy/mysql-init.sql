-- Run once on the existing MySQL server before starting PhantomFrame.
-- Canvas continues using its own canvas database on the same server.
CREATE DATABASE IF NOT EXISTS phantomframe
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_bin;
