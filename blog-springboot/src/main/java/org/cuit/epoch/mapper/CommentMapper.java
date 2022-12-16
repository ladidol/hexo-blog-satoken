package org.cuit.epoch.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Param;
import org.cuit.epoch.dto.comment.*;
import org.cuit.epoch.entity.Comment;
import org.cuit.epoch.vo.CommentQueryVO;
import org.cuit.epoch.vo.ConditionVO;
import org.springframework.stereotype.Repository;

import java.util.List;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/14 21:44
 * @description: {}
 */
@Repository
public interface CommentMapper extends BaseMapper<Comment> {

    /**
     * 查看评论
     *
     * @param current        当前页码
     * @param size           大小
     * @param commentQueryVO 评论信息
     * @return 评论集合
     */
    List<CommentDTO> listComments(@Param("current") Long current, @Param("size") Long size, @Param("commentQueryVO") CommentQueryVO commentQueryVO);

    /**
     * 查看评论id集合下的回复
     *
     * @param commentIdList 评论id集合
     * @return 回复集合
     */
    List<ReplyDTO> listReplies(@Param("commentIdList") List<Integer> commentIdList);

    /**
     * 查看当条评论下的回复
     *
     * @param commentId 评论id
     * @param current   当前页码
     * @param size      大小
     * @return 回复集合
     */
    List<ReplyDTO> listRepliesByCommentId(@Param("current") Long current, @Param("size") Long size, @Param("commentId") Integer commentId);

    /**
     * 根据评论id查询回复总量
     *
     * @param commentIdList 评论id集合
     * @return 回复数量
     */
    List<ReplyCountDTO> listReplyCountByCommentId(@Param("commentIdList") List<Integer> commentIdList);

    /**
     * 根据评论主题id获取评论量
     *
     * @param topicIdList 说说id列表
     * @return {@link List< CommentCountDTO >}说说评论量
     */
    List<CommentCountDTO> listCommentCountByTopicIds(List<Integer> topicIdList);

    /**
     * 查询后台评论
     *
     * @param current   页码
     * @param size      大小
     * @param condition 条件
     * @return 评论集合
     */
    List<CommentBackDTO> listCommentBackDTO(@Param("current") Long current, @Param("size") Long size, @Param("condition") ConditionVO condition);

    /**
     * 统计后台评论数量
     *
     * @param condition 条件
     * @return 评论数量
     */
    Integer countCommentDTO(@Param("condition") ConditionVO condition);


    List<Comment> listAllCommentsByIds(@Param("commentIdList") List<Integer> commentIdList);


}

