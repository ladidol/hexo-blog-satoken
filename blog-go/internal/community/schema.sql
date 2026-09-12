CREATE TABLE tb_comment (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  topic_id INTEGER NULL DEFAULT NULL,
  comment_content TEXT NOT NULL,
  reply_user_id INTEGER NULL DEFAULT NULL,
  parent_id INTEGER NULL DEFAULT NULL,
  type INTEGER NOT NULL,
  is_delete INTEGER NOT NULL DEFAULT 0,
  is_review INTEGER NOT NULL DEFAULT 1,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  FOREIGN KEY (user_id) REFERENCES tb_user_info(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (parent_id) REFERENCES tb_comment(id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (reply_user_id) REFERENCES tb_user_info(id) ON DELETE SET NULL DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX idx_comment_topic ON tb_comment(type,topic_id,is_review,is_delete,parent_id,create_time,id);
CREATE INDEX idx_comment_parent ON tb_comment(parent_id,is_review,create_time,id);

CREATE TABLE tb_message (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  nickname TEXT NOT NULL,
  avatar TEXT NOT NULL,
  message_content TEXT NOT NULL,
  ip_address TEXT NOT NULL,
  ip_source TEXT NOT NULL,
  time INTEGER NULL DEFAULT NULL,
  is_review INTEGER NOT NULL DEFAULT 1,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL
);
CREATE INDEX idx_message_review ON tb_message(is_review,create_time DESC,id DESC);
