package org.cuit.epoch.entity;

import com.baomidou.mybatisplus.annotation.*;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.time.LocalDateTime;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/14 21:37
 * @description: {}
 */
@Data
@Builder
@AllArgsConstructor
@NoArgsConstructor
@TableName("tb_comment")
public class Comment {

    /**
     * id
     */
    @TableId(value = "id", type = IdType.AUTO)
    private Integer id;

    /**
     * 评论用户Id
     */
    private Integer userId;

    /**
     * 回复用户id
     */
    private Integer replyUserId;

    /**
     * 评论主题id
     */
    private Integer topicId;

    /**
     * 评论内容
     */
    private String commentContent;

    /**
     * 父评论id
     */
    private Integer parentId;

    /**
     * 评论类型 1.文章 2.友链 3.说说
     */
    private Integer type;

    /**
     * 是否删除
     */
    private Integer isDelete;

    /**
     * 是否审核
     */
    private Integer isReview;

    /**
     * 创建时间
     */
    @TableField(fill = FieldFill.INSERT)
    private LocalDateTime createTime;

    /**
     * 修改时间
     */
    @TableField(fill = FieldFill.UPDATE)
    private LocalDateTime updateTime;

}