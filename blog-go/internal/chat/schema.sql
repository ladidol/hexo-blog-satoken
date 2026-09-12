CREATE TABLE tb_chat_record (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NULL DEFAULT NULL,
  nickname TEXT NOT NULL,
  avatar TEXT NOT NULL,
  content TEXT NOT NULL,
  ip_address TEXT NOT NULL,
  ip_source TEXT NOT NULL,
  type INTEGER NOT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  owner_key TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_chat_time ON tb_chat_record(create_time,id);
CREATE INDEX idx_chat_owner ON tb_chat_record(owner_key,id);
