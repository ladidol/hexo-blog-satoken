package org.cuit.epoch.dto.chat;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;
import org.cuit.epoch.entity.ChatRecord;

import java.util.List;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/26 16:44
 * @description: {}
 */
@Data
@AllArgsConstructor
@NoArgsConstructor
@Builder
public class ChatRecordDTO {

    /**
     * 聊天记录
     */
    private List<ChatRecord> chatRecordList;

    /**
     * ip地址
     */
    private String ipAddress;

    /**
     * ip来源
     */
    private String ipSource;

}