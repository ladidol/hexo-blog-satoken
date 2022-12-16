package org.cuit.epoch.service.impl;

import cn.dev33.satoken.stp.StpUtil;
import com.alibaba.fastjson.JSON;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.toolkit.CollectionUtils;
import com.baomidou.mybatisplus.core.toolkit.StringUtils;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import lombok.extern.slf4j.Slf4j;
import org.cuit.epoch.dto.EmailDTO;
import org.cuit.epoch.dto.UserDetailDTO;
import org.cuit.epoch.dto.comment.CommentBackDTO;
import org.cuit.epoch.dto.comment.CommentDTO;
import org.cuit.epoch.dto.comment.ReplyCountDTO;
import org.cuit.epoch.dto.comment.ReplyDTO;
import org.cuit.epoch.entity.Comment;
import org.cuit.epoch.mapper.ArticleMapper;
import org.cuit.epoch.mapper.CommentMapper;
import org.cuit.epoch.mapper.TalkMapper;
import org.cuit.epoch.mapper.UserInfoMapper;
import org.cuit.epoch.service.BlogInfoService;
import org.cuit.epoch.service.CommentService;
import org.cuit.epoch.service.RedisService;
import org.cuit.epoch.util.HTMLUtils;
import org.cuit.epoch.util.PageUtils;
import org.cuit.epoch.vo.*;
import org.cuit.epoch.vo.page.PageResult;
import org.springframework.amqp.core.Message;
import org.springframework.amqp.core.MessageProperties;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.concurrent.CompletableFuture;
import java.util.stream.Collectors;

import static org.cuit.epoch.constant.CommonConst.*;
import static org.cuit.epoch.constant.MQPrefixConst.EMAIL_EXCHANGE;
import static org.cuit.epoch.constant.RedisPrefixConst.*;
import static org.cuit.epoch.enums.CommentTypeEnum.getCommentEnum;
import static org.cuit.epoch.enums.CommentTypeEnum.getCommentPath;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/14 21:42
 * @description: {}
 */
@Service
@Slf4j
public class CommentServiceImpl extends ServiceImpl<CommentMapper, Comment> implements CommentService {
    @Autowired
    private CommentMapper commentDao;
    @Autowired
    private ArticleMapper articleDao;
    @Autowired
    private TalkMapper talkDao;
    @Autowired
    private RedisService redisService;
    @Autowired
    private UserInfoMapper userInfoDao;
    @Autowired
    private RabbitTemplate rabbitTemplate;
    @Autowired
    private BlogInfoService blogInfoService;

    /**
     * 网站网址
     */
    @Value("${website.url}")
    private String websiteUrl;

    @Override
    public PageResult<CommentDTO> listComments(CommentQueryVO commentQueryVO) {
        // 查询评论量
        Integer commentCount = commentDao.selectCount(new LambdaQueryWrapper<Comment>()
                .eq(Objects.nonNull(commentQueryVO.getTopicId()), Comment::getTopicId, commentQueryVO.getTopicId())
                .eq(Comment::getType, commentQueryVO.getType())
                .isNull(Comment::getParentId)
                .eq(Comment::getIsReview, TRUE));
        if (commentCount == 0) {
            return new PageResult<>();
        }
        // 分页查询评论数据
        List<CommentDTO> commentDTOList = commentDao.listComments(PageUtils.getLimitCurrent(), PageUtils.getSize(), commentQueryVO);
        if (CollectionUtils.isEmpty(commentDTOList)) {
            return new PageResult<>();
        }
        // 提取评论id集合
        List<Integer> commentIdList = commentDTOList.stream()
                .map(CommentDTO::getId)
                .collect(Collectors.toList());
        // 查询redis的评论点赞数据
        Map<String, Object> likeCountMap = redisService.hGetAll(COMMENT_LIKE_COUNT);
        // 根据评论id集合查询回复数据
        List<ReplyDTO> replyDTOList = commentDao.listReplies(commentIdList);
        // 封装回复点赞量
        replyDTOList.forEach(item -> item.setLikeCount((Integer) likeCountMap.get(item.getId().toString())));
        // 根据父评论id分组回复
        Map<Integer, List<ReplyDTO>> replyMap = replyDTOList.stream()
                .collect(Collectors.groupingBy(ReplyDTO::getParentId));
        // 根据评论id查询回复量
        Map<Integer, Integer> replyCountMap = commentDao.listReplyCountByCommentId(commentIdList)
                .stream().collect(Collectors.toMap(ReplyCountDTO::getCommentId, ReplyCountDTO::getReplyCount));
        // 封装评论数据
        commentDTOList.forEach(item -> {
            item.setLikeCount((Integer) likeCountMap.get(item.getId().toString()));
            item.setReplyDTOList(replyMap.get(item.getId()));
            item.setReplyCount(replyCountMap.get(item.getId()));
        });
        return new PageResult<>(commentDTOList, commentCount);
    }

    @Override
    public List<ReplyDTO> listRepliesByCommentId(Integer commentId) {
        // 转换页码查询评论下的回复
        List<ReplyDTO> replyDTOList = commentDao.listRepliesByCommentId(PageUtils.getLimitCurrent(), PageUtils.getSize(), commentId);
        // 查询redis的评论点赞数据
        Map<String, Object> likeCountMap = redisService.hGetAll(COMMENT_LIKE_COUNT);
        // 封装点赞数据
        replyDTOList.forEach(item -> item.setLikeCount((Integer) likeCountMap.get(item.getId().toString())));
        return replyDTOList;
    }

    @Override
    public void saveComment(CommentVO commentVO) {
        // 判断是否需要审核
        WebsiteConfigVO websiteConfig = blogInfoService.getWebsiteConfig();
        Integer isReview = websiteConfig.getIsCommentReview();
        // 过滤标签
        commentVO.setCommentContent(HTMLUtils.filter(commentVO.getCommentContent()));
        // 拿到用户信息表
        UserDetailDTO userDetailDTO = (UserDetailDTO) StpUtil.getSession().get(USER_INFO);
        Comment comment = Comment.builder()
                .userId(userDetailDTO.getUserInfoId())//用户信息id
                .replyUserId(commentVO.getReplyUserId())
                .topicId(commentVO.getTopicId())
                .commentContent(commentVO.getCommentContent())
                .parentId(commentVO.getParentId())
                .type(commentVO.getType())
                .isReview(isReview == TRUE ? FALSE : TRUE)
                .build();
        commentDao.insert(comment);
        // 判断是否开启邮箱通知,通知用户
        if (websiteConfig.getIsEmailNotice().equals(TRUE)) {
            CompletableFuture.runAsync(() -> notice(comment));
        }
    }

    @Override
    public void saveCommentLike(Integer commentId) {
        // 拿到用户信息表 + 拼接成redis中点赞key
        UserDetailDTO userDetailDTO = (UserDetailDTO) StpUtil.getSession().get(USER_INFO);
        String commentLikeKey = COMMENT_USER_LIKE + userDetailDTO.getUserInfoId();
        // 判断是否点赞
        if (redisService.sIsMember(commentLikeKey, commentId)) {
            // 点过赞则删除评论id
            redisService.sRemove(commentLikeKey, commentId);
            // 评论点赞量-1
            redisService.hDecr(COMMENT_LIKE_COUNT, commentId.toString(), 1L);
        } else {
            // 未点赞则增加评论id
            redisService.sAdd(commentLikeKey, commentId);
            // 评论点赞量+1
            redisService.hIncr(COMMENT_LIKE_COUNT, commentId.toString(), 1L);
        }
    }

    @Override
    public void updateCommentsReview(ReviewVO reviewVO) {
        // 修改评论审核状态
        List<Comment> commentList = reviewVO.getIdList().stream().map(item -> Comment.builder()
                        .id(item)
                        .isReview(reviewVO.getIsReview())
                        .build())
                .collect(Collectors.toList());
        this.updateBatchById(commentList);

        log.info("开始通知已经审核被回复的用户");
        List<Integer> ids = commentList.stream().map(Comment::getId).collect(Collectors.toList());
        List<Comment> comments = commentDao.listAllCommentsByIds(ids);

        WebsiteConfigVO websiteConfig = blogInfoService.getWebsiteConfig();
        // 判断是否开启邮箱通知,通知这些用户
        if (websiteConfig.getIsEmailNotice().equals(TRUE)) {
            for (Comment comment : comments) {
                // 2022/12/16 这里可以看一下，审核完后的评论再一次邮箱提醒收件人。 通过开不开线程能知道，runAsync这个方法可以阻止报错产生，以至于不会影响主线程
                CompletableFuture.runAsync(() -> notice(comment));
            }
        }
    }

    @Override
    public PageResult<CommentBackDTO> listCommentBackDTO(ConditionVO condition) {
        // 统计后台评论量
        Integer count = commentDao.countCommentDTO(condition);
        if (count == 0) {
            return new PageResult<>();
        }
        // 查询后台评论集合
        List<CommentBackDTO> commentBackDTOList = commentDao.listCommentBackDTO(PageUtils.getLimitCurrent(), PageUtils.getSize(), condition);
        return new PageResult<>(commentBackDTOList, count);
    }

    /**
     * 邮箱通知评论用户
     *
     * @param comment 评论信息
     */
    public void notice(Comment comment) {
        // 查询回复用户邮箱号
        Integer userId = BLOGGER_ID;
        String id = Objects.nonNull(comment.getTopicId()) ? comment.getTopicId().toString() : "";
        if (Objects.nonNull(comment.getReplyUserId())) {
            userId = comment.getReplyUserId();
        } else {
            switch (Objects.requireNonNull(getCommentEnum(comment.getType()))) {
                case ARTICLE:
                    userId = articleDao.selectById(comment.getTopicId()).getUserId();
                    break;
                case TALK:
                    userId = talkDao.selectById(comment.getTopicId()).getUserId();
                    break;
                default:
                    break;
            }
        }
        String email = userInfoDao.selectById(userId).getEmail();
        if (StringUtils.isNotBlank(email)) {
            // 发送消息
            EmailDTO emailDTO = new EmailDTO();
            if (comment.getIsReview().equals(TRUE)) {
                // 评论提醒
                emailDTO.setEmail(email);
                emailDTO.setSubject("评论提醒");
                // 获取评论路径
                String url = websiteUrl + getCommentPath(comment.getType()) + id;
                emailDTO.setContent("您收到了一条新的回复，请前往" + url + "\n页面查看");
            } else {
                // 管理员审核提醒
                String adminEmail = userInfoDao.selectById(BLOGGER_ID).getEmail();
                emailDTO.setEmail(adminEmail);
                emailDTO.setSubject("审核提醒");
                emailDTO.setContent("您收到了一条新的回复，请前往后台管理页面审核");
            }
            rabbitTemplate.convertAndSend(EMAIL_EXCHANGE, "*", new Message(JSON.toJSONBytes(emailDTO), new MessageProperties()));
        }
    }

}
