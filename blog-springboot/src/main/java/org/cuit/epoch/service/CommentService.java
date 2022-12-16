package org.cuit.epoch.service;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.dto.comment.CommentBackDTO;
import org.cuit.epoch.dto.comment.CommentDTO;
import org.cuit.epoch.dto.comment.ReplyDTO;
import org.cuit.epoch.entity.Comment;
import org.cuit.epoch.vo.CommentQueryVO;
import org.cuit.epoch.vo.CommentVO;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.ReviewVO;
import org.cuit.epoch.vo.page.PageResult;

import java.util.List;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/14 21:37
 * @description: {}
 */
public interface CommentService extends IService<Comment> {

    /**
     * 查看评论
     *
     * @param commentVO 评论信息
     * @return 评论列表
     */
    PageResult<CommentDTO> listComments(CommentQueryVO commentVO);

    /**
     * 查看评论下的回复
     *
     * @param commentId 评论id
     * @return 回复列表
     */
    List<ReplyDTO> listRepliesByCommentId(Integer commentId);

    /**
     * 添加评论
     *
     * @param commentVO 评论对象
     */
    void saveComment(CommentVO commentVO);

    /**
     * 点赞评论
     *
     * @param commentId 评论id
     */
    void saveCommentLike(Integer commentId);

    /**
     * 审核评论
     *
     * @param reviewVO 审核信息
     */
    void updateCommentsReview(ReviewVO reviewVO);

    /**
     * 查询后台评论
     *
     * @param condition 条件
     * @return 评论列表
     */
    PageResult<CommentBackDTO> listCommentBackDTO(ConditionVO condition);

}
