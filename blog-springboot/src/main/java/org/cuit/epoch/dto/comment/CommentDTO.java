package org.cuit.epoch.dto.comment;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.time.LocalDateTime;
import java.util.List;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/14 21:38
 * @description: {}
 */
@Data
@Builder
@AllArgsConstructor
@NoArgsConstructor
public class CommentDTO {

    /**
     * 评论id
     */
    private Integer id;

    /**
     * 用户id
     */
    private Integer userId;

    /**
     * 用户昵称
     */
    private String nickname;

    /**
     * 用户头像
     */
    private String avatar;

    /**
     * 个人网站
     */
    private String webSite;

    /**
     * 评论内容
     */
    private String commentContent;

    /**
     * 点赞数
     */
    private Integer likeCount;

    /**
     * 评论时间
     */
    private LocalDateTime createTime;

    /**
     * 回复量
     */
    private Integer replyCount;

    /**
     * 回复列表
     */
    private List<ReplyDTO> replyDTOList;

}