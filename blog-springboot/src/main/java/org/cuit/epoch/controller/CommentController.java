package org.cuit.epoch.controller;

import io.swagger.annotations.Api;
import io.swagger.annotations.ApiImplicitParam;
import io.swagger.annotations.ApiOperation;
import org.cuit.epoch.annotation.OptLog;
import org.cuit.epoch.dto.comment.CommentBackDTO;
import org.cuit.epoch.dto.comment.CommentDTO;
import org.cuit.epoch.dto.comment.ReplyDTO;
import org.cuit.epoch.service.CommentService;
import org.cuit.epoch.util.Result;
import org.cuit.epoch.vo.CommentQueryVO;
import org.cuit.epoch.vo.CommentVO;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.ReviewVO;
import org.cuit.epoch.vo.page.PageResult;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.*;

import javax.validation.Valid;
import java.util.List;

import static org.cuit.epoch.constant.OptTypeConst.REMOVE;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/14 21:36
 * @description: {}
 */
@Api(tags = "评论模块")
@RestController
public class CommentController {
    @Autowired
    private CommentService commentService;

    /**
     * 查询评论
     *
     * @param commentQueryVO 评论信息
     * @return {@link Result<CommentDTO>}
     */
    @ApiOperation(value = "查询当前页面的评论")
    @GetMapping("/comments")
    public Result<PageResult<CommentDTO>> listComments(CommentQueryVO commentQueryVO) {
        return Result.ok(commentService.listComments(commentQueryVO));
    }

    /**
     * 添加评论
     *
     * @param commentVO 评论信息
     * @return {@link Result<>}
     */
    @ApiOperation(value = "添加评论")
    @PostMapping("/comments")
    public Result<?> saveComment(@Valid @RequestBody CommentVO commentVO) {
        commentService.saveComment(commentVO);
        return Result.ok();
    }

    /**
     * 查询评论下的回复
     *
     * @param commentId 评论id
     * @return {@link Result< ReplyDTO >} 回复列表
     */
    @ApiOperation(value = "查询评论下的回复")
    @ApiImplicitParam(name = "commentId", value = "评论id", required = true, dataType = "Integer")
    @GetMapping("/comments/{commentId}/replies")
    public Result<List<ReplyDTO>> listRepliesByCommentId(@PathVariable("commentId") Integer commentId) {
        return Result.ok(commentService.listRepliesByCommentId(commentId));
    }

    /**
     * 评论点赞
     *
     * @param commentId 评论id
     * @return {@link Result<>}
     */
    @ApiOperation(value = "评论点赞")
    @PostMapping("/comments/{commentId}/like")
    public Result<?> saveCommentLike(@PathVariable("commentId") Integer commentId) {
        commentService.saveCommentLike(commentId);
        return Result.ok();
    }

    /**
     * 审核评论
     *
     * @param reviewVO 审核信息
     * @return {@link Result<>}
     */
    @OptLog(optType = REMOVE)
    @ApiOperation(value = "审核评论")
    @PutMapping("/admin/comments/review")

    public Result<?> updateCommentsReview(@Valid @RequestBody ReviewVO reviewVO) {
        commentService.updateCommentsReview(reviewVO);
        return Result.ok();
    }

    /**
     * 删除评论
     *
     * @param commentIdList 评论id列表
     * @return {@link Result<>}
     */
    @OptLog(optType = REMOVE)
    @ApiOperation(value = "删除评论")
    @DeleteMapping("/admin/comments")
    public Result<?> deleteComments(@RequestBody List<Integer> commentIdList) {
        commentService.removeByIds(commentIdList);
        return Result.ok();
    }

    /**
     * 查询后台评论
     *
     * @param condition 条件
     * @return {@link Result< CommentBackDTO >} 后台评论
     */
    @ApiOperation(value = "查询后台评论")
    @GetMapping("/admin/comments")
    public Result<PageResult<CommentBackDTO>> listCommentBackDTO(ConditionVO condition) {
        return Result.ok(commentService.listCommentBackDTO(condition));
    }

}