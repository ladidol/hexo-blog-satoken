package org.cuit.epoch.service.impl;

import cn.dev33.satoken.stp.StpUtil;
import com.alibaba.fastjson.JSON;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.toolkit.IdWorker;
import com.baomidou.mybatisplus.core.toolkit.StringUtils;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import eu.bitwalker.useragentutils.UserAgent;
import lombok.extern.slf4j.Slf4j;
import org.cuit.epoch.dto.EmailDTO;
import org.cuit.epoch.dto.UserDetailDTO;
import org.cuit.epoch.dto.UserInfoDTO;
import org.cuit.epoch.entity.UserAuth;
import org.cuit.epoch.entity.UserInfo;
import org.cuit.epoch.entity.UserRole;
import org.cuit.epoch.enums.CommonConst;

import org.cuit.epoch.enums.LoginTypeEnum;
import org.cuit.epoch.enums.RoleEnum;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.mapper.RoleMapper;
import org.cuit.epoch.mapper.UserAuthMapper;

import org.cuit.epoch.mapper.UserInfoMapper;
import org.cuit.epoch.mapper.UserRoleMapper;
import org.cuit.epoch.service.BlogInfoService;
import org.cuit.epoch.service.RedisService;
import org.cuit.epoch.service.UserAuthService;
import org.cuit.epoch.strategy.context.SocialLoginStrategyContext;
import org.cuit.epoch.util.IpUtils;
import org.cuit.epoch.util.PasswordUtils;
import org.cuit.epoch.vo.UserVO;
import org.cuit.epoch.vo.strategy.login.WeiboLoginVO;
import org.springframework.amqp.core.Message;
import org.springframework.amqp.core.MessageProperties;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.scheduling.annotation.Async;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import javax.servlet.http.HttpServletRequest;
import java.time.LocalDateTime;
import java.time.ZoneId;
import java.util.*;

import static org.cuit.epoch.enums.MQPrefixConst.EMAIL_EXCHANGE;
import static org.cuit.epoch.enums.RedisPrefixConst.*;
import static org.cuit.epoch.enums.RedisPrefixConst.TALK_USER_LIKE;
import static org.cuit.epoch.enums.ZoneEnum.SHANGHAI;
import static org.cuit.epoch.util.CommonUtils.checkEmail;
import static org.cuit.epoch.util.CommonUtils.getRandomCode;

/**
 * @author: ladidol
 * @date: 2022/11/15 21:48
 * @description:
 */
@Service
@Slf4j
public class UserAuthServiceImpl extends ServiceImpl<UserAuthMapper, UserAuth> implements UserAuthService {

    @Autowired
    private RedisService redisService;
    @Autowired
    private UserAuthMapper userAuthMapper;

    @Autowired
    private HttpServletRequest request;

    @Autowired
    private RoleMapper roleMapper;

    @Autowired
    private UserRoleMapper userRoleMapper;
    @Autowired
    private UserInfoMapper userInfoMapper;
    @Autowired
    private BlogInfoService blogInfoService;
    @Autowired
    private RabbitTemplate rabbitTemplate;
    @Autowired
    private SocialLoginStrategyContext socialLoginStrategyContext;

    @Transactional(rollbackFor = Exception.class)
    @Override
    public UserDetailDTO login(String username, String password) {
        if (StringUtils.isBlank(username)) {
            throw new AppException("用户名不能为空！");
        }
        // 查询账号是否存在
        UserAuth userAuth = userAuthMapper.selectOne(new LambdaQueryWrapper<UserAuth>()
                .select(UserAuth::getId, UserAuth::getUserInfoId, UserAuth::getUsername, UserAuth::getPassword, UserAuth::getLoginType)
                .eq(UserAuth::getUsername, username));
        if (Objects.isNull(userAuth)) {
            throw new AppException("用户名不存在!");
        }
        // 封装登录信息
        UserDetailDTO userDetailDTO = convertUserDetail(userAuth, request);
        log.info("userDetailDTO = " + userDetailDTO);

        if (!userDetailDTO.getPassword().equals(PasswordUtils.encrypt(password))) {
            throw new AppException("密码错误！");
        }
        //sa-token登录
        StpUtil.login(userDetailDTO.getId());
        //将用户角色信息存入session中
        StpUtil.getSession().set(USER_ROLE,userDetailDTO.getRoleList());
        //将用户详细信息存入session中
        StpUtil.getSession().set(USER_INFO,userDetailDTO);

//        //用户redis角色添加
//        redisService.set(USER_ROLE + StpUtil.getLoginId(), userDetailDTO.getRoleList());
//        //用户redis信息添加
//        redisService.set(USER_INFO + StpUtil.getLoginId(),userDetailDTO);


        // 更新用户ip，最近登录时间
        updateUserInfo(userDetailDTO);

        return userDetailDTO;
    }

    @Transactional(rollbackFor = Exception.class)
    @Override
    public void logout() {

        // 2022/11/22 这里就不需要用redis来存用户信息了
//        //删除用户redis中角色信息
//        redisService.del(USER_ROLE + StpUtil.getLoginId());
//        //删除用户redis中详细信息
//        redisService.del(USER_INFO + StpUtil.getLoginId());
        //sa-token注销
        StpUtil.logout();

    }

    /**
     * 封装用户登录信息
     *
     * @param user    用户账号
     * @param request 请求
     * @return 用户登录信息
     */
    public UserDetailDTO convertUserDetail(UserAuth user, HttpServletRequest request) {
        // 查询账号信息
        UserInfo userInfo = userInfoMapper.selectById(user.getUserInfoId());
        // 查询账号角色
        List<String> roleList = roleMapper.listRolesByUserInfoId(userInfo.getId());
        // 查询账号点赞信息
        Set<Object> articleLikeSet = redisService.sMembers(ARTICLE_USER_LIKE + userInfo.getId());
        Set<Object> commentLikeSet = redisService.sMembers(COMMENT_USER_LIKE + userInfo.getId());
        Set<Object> talkLikeSet = redisService.sMembers(TALK_USER_LIKE + userInfo.getId());
        // 获取设备信息
        String ipAddress = IpUtils.getIpAddress(request);
        String ipSource = IpUtils.getIpSource(ipAddress);
        UserAgent userAgent = IpUtils.getUserAgent(request);
        // 封装权限集合
        return UserDetailDTO.builder()
                .id(user.getId())
                .loginType(user.getLoginType())
                .userInfoId(userInfo.getId())
                .username(user.getUsername())
                .password(user.getPassword())
                .email(userInfo.getEmail())
                .roleList(roleList)
                .nickname(userInfo.getNickname())
                .avatar(userInfo.getAvatar())
                .intro(userInfo.getIntro())
                .webSite(userInfo.getWebSite())
                .articleLikeSet(articleLikeSet)
                .commentLikeSet(commentLikeSet)
                .talkLikeSet(talkLikeSet)
                .ipAddress(ipAddress)
                .ipSource(ipSource)
                .isDisable(userInfo.getIsDisable())
                .browser(userAgent.getBrowser().getName())
                .os(userAgent.getOperatingSystem().getName())
                .lastLoginTime(LocalDateTime.now(ZoneId.of(SHANGHAI.getZone())))
                .build();
    }

    /**
     * 登录成功后 更新用户信息
     */
    @Async
    public void updateUserInfo(UserDetailDTO userDetailDTO) {
        UserAuth userAuth = UserAuth.builder()
                .id(userDetailDTO.getId())
                .ipAddress(userDetailDTO.getIpAddress())
                .ipSource(userDetailDTO.getIpSource())
                .lastLoginTime(userDetailDTO.getLastLoginTime())
                .build();
        userAuthMapper.updateById(userAuth);
    }

    @Transactional(rollbackFor = Exception.class)
    @Override
    public void register(UserVO user) {
        // 校验账号是否合法
        if (checkUser(user)) {
            throw new AppException("邮箱已被注册！");
        }
        // 新增用户信息
        UserInfo userInfo = UserInfo.builder()
                .email(user.getUsername())
                .nickname(CommonConst.DEFAULT_NICKNAME + IdWorker.getId())
                .avatar(blogInfoService.getWebsiteConfig().getUserAvatar())
                .build();
        userInfoMapper.insert(userInfo);
        // 绑定用户角色
        UserRole userRole = UserRole.builder()
                .userId(userInfo.getId())
                .roleId(RoleEnum.USER.getRoleId())
                .build();
        userRoleMapper.insert(userRole);
        // 新增用户账号
        UserAuth userAuth = UserAuth.builder()
                .userInfoId(userInfo.getId())
                .username(user.getUsername())
                .password(PasswordUtils.encrypt(user.getPassword()))
                .loginType(LoginTypeEnum.EMAIL.getType())
                .build();
        userAuthMapper.insert(userAuth);
    }

    /**
     * 校验用户数据是否合法
     *
     * @param user 用户数据
     * @return 结果
     */
    private Boolean checkUser(UserVO user) {
//        if (!user.getCode().equals(redisService.get(USER_CODE_KEY + user.getUsername()))) {
//            throw new AppException("验证码错误！");
//        }
        // TODO: 2022/11/18 开发期间先将邮箱验证关闭

        //查询用户名是否存在
        UserAuth userAuth = userAuthMapper.selectOne(new LambdaQueryWrapper<UserAuth>()
                .select(UserAuth::getUsername)
                .eq(UserAuth::getUsername, user.getUsername()));
        return Objects.nonNull(userAuth);
    }


    @Override
    public void sendCode(String username) {
        // 校验账号是否合法
        if (!checkEmail(username)) {
            throw new AppException("请输入正确邮箱");
        }
        // 生成六位随机验证码发送
        String code = getRandomCode();
        // 发送验证码
        EmailDTO emailDTO = EmailDTO.builder()
                .email(username)
                .subject("ladidol'blog 的验证码")
                .content("您的验证码为 " + code + " 有效期15分钟，请不要告诉他人哦！")
                .build();
        rabbitTemplate.convertAndSend(EMAIL_EXCHANGE, "*", new Message(JSON.toJSONBytes(emailDTO), new MessageProperties()));
        // 将验证码存入redis，设置过期时间为15分钟
        redisService.set(USER_CODE_KEY + username, code, CODE_EXPIRE_TIME);
    }
//
//    @Transactional(rollbackFor = Exception.class)
//    @Override
//    public UserInfoDTO qqLogin(QQLoginVO qqLoginVO) {
//        return socialLoginStrategyContext.executeLoginStrategy(JSON.toJSONString(qqLoginVO), LoginTypeEnum.QQ);
//    }

    @Transactional(rollbackFor = AppException.class)
    @Override
    public UserInfoDTO weiboLogin(WeiboLoginVO weiboLoginVO) {
        return socialLoginStrategyContext.executeLoginStrategy(JSON.toJSONString(weiboLoginVO), LoginTypeEnum.WEIBO);
    }




//
//    @Override
//    public List<UserAreaDTO> listUserAreas(ConditionVO conditionVO) {
//        List<UserAreaDTO> userAreaDTOList = new ArrayList<>();
//        switch (Objects.requireNonNull(getUserAreaType(conditionVO.getType()))) {
//            case USER:
//                // 查询注册用户区域分布
//                Object userArea = redisService.get(USER_AREA);
//                if (Objects.nonNull(userArea)) {
//                    userAreaDTOList = JSON.parseObject(userArea.toString(), List.class);
//                }
//                return userAreaDTOList;
//            case VISITOR:
//                // 查询游客区域分布
//                Map<String, Object> visitorArea = redisService.hGetAll(VISITOR_AREA);
//                if (Objects.nonNull(visitorArea)) {
//                    userAreaDTOList = visitorArea.entrySet().stream()
//                            .map(item -> UserAreaDTO.builder()
//                                    .name(item.getKey())
//                                    .value(Long.valueOf(item.getValue().toString()))
//                                    .build())
//                            .collect(Collectors.toList());
//                }
//                return userAreaDTOList;
//            default:
//                break;
//        }
//        return userAreaDTOList;
//    }

//
//    @Override
//    public void updatePassword(UserVO user) {
//        // 校验账号是否合法
//        if (!checkUser(user)) {
//            throw new BizException("邮箱尚未注册！");
//        }
//        // 根据用户名修改密码
//        userAuthDao.update(new UserAuth(), new LambdaUpdateWrapper<UserAuth>()
//                .set(UserAuth::getPassword, BCrypt.hashpw(user.getPassword(), BCrypt.gensalt()))
//                .eq(UserAuth::getUsername, user.getUsername()));
//    }
//
//    @Override
//    public void updateAdminPassword(PasswordVO passwordVO) {
//        // 查询旧密码是否正确
//        UserAuth user = userAuthDao.selectOne(new LambdaQueryWrapper<UserAuth>()
//                .eq(UserAuth::getId, UserUtils.getLoginUser().getId()));
//        // 正确则修改密码，错误则提示不正确
//        if (Objects.nonNull(user) && BCrypt.checkpw(passwordVO.getOldPassword(), user.getPassword())) {
//            UserAuth userAuth = UserAuth.builder()
//                    .id(UserUtils.getLoginUser().getId())
//                    .password(BCrypt.hashpw(passwordVO.getNewPassword(), BCrypt.gensalt()))
//                    .build();
//            userAuthDao.updateById(userAuth);
//        } else {
//            throw new BizException("旧密码不正确");
//        }
//    }
//
//    @Override
//    public PageResult<UserBackDTO> listUserBackDTO(ConditionVO condition) {
//        // 获取后台用户数量
//        Integer count = userAuthDao.countUser(condition);
//        if (count == 0) {
//            return new PageResult<>();
//        }
//        // 获取后台用户列表
//        List<UserBackDTO> userBackDTOList = userAuthDao.listUsers(PageUtils.getLimitCurrent(), PageUtils.getSize(), condition);
//        return new PageResult<>(userBackDTOList, count);
//    }
//
//    @Transactional(rollbackFor = Exception.class)
//    @Override
//    public UserInfoDTO qqLogin(QQLoginVO qqLoginVO) {
//        return socialLoginStrategyContext.executeLoginStrategy(JSON.toJSONString(qqLoginVO), LoginTypeEnum.QQ);
//    }
//
//    @Transactional(rollbackFor = BizException.class)
//    @Override
//    public UserInfoDTO weiboLogin(WeiboLoginVO weiboLoginVO) {
//        return socialLoginStrategyContext.executeLoginStrategy(JSON.toJSONString(weiboLoginVO), LoginTypeEnum.WEIBO);
//    }
//
//    /**
//     * 校验用户数据是否合法
//     *
//     * @param user 用户数据
//     * @return 结果
//     */
//    private Boolean checkUser(UserVO user) {
//        if (!user.getCode().equals(redisService.get(USER_CODE_KEY + user.getUsername()))) {
//            throw new BizException("验证码错误！");
//        }
//        //查询用户名是否存在
//        UserAuth userAuth = userAuthDao.selectOne(new LambdaQueryWrapper<UserAuth>()
//                .select(UserAuth::getUsername)
//                .eq(UserAuth::getUsername, user.getUsername()));
//        return Objects.nonNull(userAuth);
//    }
//
//    /**
//     * 统计用户地区
//     */
//    @Scheduled(cron = "0 0 * * * ?")
//    public void statisticalUserArea() {
//        // 统计用户地域分布
//        Map<String, Long> userAreaMap = userAuthDao.selectList(new LambdaQueryWrapper<UserAuth>().select(UserAuth::getIpSource))
//                .stream()
//                .map(item -> {
//                    if (StringUtils.isNotBlank(item.getIpSource())) {
//                        return item.getIpSource().substring(0, 2)
//                                .replaceAll(PROVINCE, "")
//                                .replaceAll(CITY, "");
//                    }
//                    return UNKNOWN;
//                })
//                .collect(Collectors.groupingBy(item -> item, Collectors.counting()));
//        // 转换格式
//        List<UserAreaDTO> userAreaList = userAreaMap.entrySet().stream()
//                .map(item -> UserAreaDTO.builder()
//                        .name(item.getKey())
//                        .value(item.getValue())
//                        .build())
//                .collect(Collectors.toList());
//        redisService.set(USER_AREA, JSON.toJSONString(userAreaList));
//    }

}